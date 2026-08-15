package collector

import (
	"context"
	"log/slog"
	"time"

	"github.com/rinat1313/zakupki-collector/internal/config"
	"github.com/rinat1313/zakupki-collector/internal/eis"
	"github.com/rinat1313/zakupki-collector/internal/model"
	"github.com/rinat1313/zakupki-collector/internal/store"
)

// Runner — сервис периодического сбора тендеров из ЕИС.
type Runner struct {
	cfg    config.Config
	client *eis.Client
	store  *store.Store
	log    *slog.Logger
}

func New(cfg config.Config, client *eis.Client, st *store.Store, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{cfg: cfg, client: client, store: st, log: log}
}

func (r *Runner) Run(ctx context.Context) error {
	r.log.Info("collector started",
		"interval", r.cfg.EISInterval.String(),
		"lookback", r.cfg.EISLookback.String(),
		"region", r.cfg.EISOrgRegion,
	)

	// первый прогон сразу
	if err := r.CollectOnce(ctx); err != nil {
		r.log.Error("initial collect failed", "err", err)
	}

	ticker := time.NewTicker(r.cfg.EISInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("collector stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := r.CollectOnce(ctx); err != nil {
				r.log.Error("collect failed", "err", err)
			}
		}
	}
}

func (r *Runner) CollectOnce(ctx context.Context) error {
	now := time.Now()
	notOlderThan := now.Add(-r.cfg.EISLookback)
	fromHour, toHour := todayHours(now, r.cfg.EISTimezone)

	r.log.Info("collect cycle",
		"from", notOlderThan.Format(time.RFC3339),
		"to", now.Format(time.RFC3339),
		"hours", []int{fromHour, toHour},
	)

	var (
		inserted, updated, skipped, failed int
	)

	for _, docType := range r.cfg.EISDocTypes {
		req := eis.RegionRequest{
			OrgRegion:    r.cfg.EISOrgRegion,
			Subsystem:    r.cfg.EISSubsystem,
			DocumentType: docType,
			FromHour:     fromHour,
			ToHour:       toHour,
			Timezone:     r.cfg.EISTimezone,
			AllOrgs:      true,
		}
		resp, err := r.client.GetPublicDocsByOrgRegion(ctx, req)
		if err != nil {
			r.log.Error("getPublicDocsByOrgRegion", "docType", docType, "err", err)
			failed++
			continue
		}
		if resp.NoData && len(resp.ArchiveURLs) == 0 {
			// async: пробуем prepared part
			prep, err := r.client.GetPreparedPart(ctx)
			if err != nil {
				r.log.Warn("getPreparedPart", "err", err)
				continue
			}
			resp = prep
		}
		if resp.NoData || len(resp.ArchiveURLs) == 0 {
			r.log.Info("no data", "docType", docType)
			continue
		}

		for _, url := range resp.ArchiveURLs {
			data, err := r.client.DownloadArchive(ctx, url)
			if err != nil {
				r.log.Error("download archive", "url", url, "err", err)
				failed++
				continue
			}
			tenders, err := eis.ParseArchives(data, "44", notOlderThan)
			if err != nil {
				r.log.Error("parse archive", "err", err)
				failed++
				continue
			}
			for i := range tenders {
				res, err := r.store.Upsert(ctx, &tenders[i])
				if err != nil {
					r.log.Error("upsert", "number", tenders[i].PurchaseNumber, "err", err)
					failed++
					continue
				}
				switch res {
				case model.UpsertInserted:
					inserted++
				case model.UpsertUpdated:
					updated++
				case model.UpsertSkipped:
					skipped++
				}
			}
		}
	}

	r.log.Info("collect done",
		"inserted", inserted,
		"updated", updated,
		"skipped", skipped,
		"failed", failed,
	)
	return nil
}

// todayHours возвращает fromHour/toHour для todayInfo с запасом под окно lookback.
func todayHours(now time.Time, tzOffset int) (fromHour, toHour int) {
	loc := time.FixedZone("EIS", tzOffset*3600)
	local := now.In(loc)
	toHour = local.Hour() + 1
	if toHour > 24 {
		toHour = 24
	}
	fromHour = toHour - 2
	if fromHour < 0 {
		fromHour = 0
	}
	return fromHour, toHour
}
