package eis

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/rinat1313/zakupki-collector/internal/model"
)

// ParseArchives извлекает тендеры из ZIP/XML ответов ЕИС (fcsExport / извещения).
func ParseArchives(data []byte, law string, notOlderThan time.Time) ([]model.Tender, error) {
	if len(data) >= 2 && data[0] == 'P' && data[1] == 'K' {
		return parseZip(data, law, notOlderThan)
	}
	return parseXMLDocuments(data, law, notOlderThan)
}

func parseZip(data []byte, law string, notOlderThan time.Time) ([]model.Tender, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	var all []model.Tender
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		items, err := parseXMLDocuments(body, law, notOlderThan)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		all = append(all, items...)
	}
	return all, nil
}

func parseXMLDocuments(data []byte, law string, notOlderThan time.Time) ([]model.Tender, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out []model.Tender
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if !isNotificationRoot(se.Name.Local) {
			continue
		}
		var raw notificationXML
		if err := dec.DecodeElement(&raw, &se); err != nil {
			// пропускаем битый фрагмент, продолжаем
			continue
		}
		t, ok := raw.toTender(law, se.Name.Local)
		if !ok {
			continue
		}
		if !notOlderThan.IsZero() && t.LastUpdatedAt != nil && t.LastUpdatedAt.Before(notOlderThan) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func isNotificationRoot(local string) bool {
	l := strings.ToLower(local)
	return strings.Contains(l, "notification") ||
		l == "purchasenotice" ||
		l == "purchasenoticeaesmbo" ||
		l == "purchasenoticezas" ||
		strings.HasPrefix(l, "epnotification") ||
		strings.HasPrefix(l, "fcsnotification")
}

type notificationXML struct {
	PurchaseNumber     string `xml:"purchaseNumber"`
	PurchaseObjectInfo string `xml:"purchaseObjectInfo"`
	DocPublishDate     string `xml:"docPublishDate"`
	Href               string `xml:"href"`
	VersionNumber      string `xml:"versionNumber"`
	ModificationDate   string `xml:"modificationDate"`

	PurchaseResponsible struct {
		ResponsibleOrg struct {
			FullName string `xml:"fullName"`
			INN      string `xml:"INN"`
		} `xml:"responsibleOrg"`
	} `xml:"purchaseResponsible"`

	Customer struct {
		FullName string `xml:"fullName"`
		INN      string `xml:"INN"`
	} `xml:"customer"`

	Lot struct {
		MaxPrice string `xml:"maxPrice"`
	} `xml:"lot"`

	NotificationInfo struct {
		ProcedureInfo struct {
			Collecting struct {
				EndDate string `xml:"endDate"`
				EndDT   string `xml:"endDateTime"`
			} `xml:"collecting"`
		} `xml:"procedureInfo"`
		ContractInfo struct {
			MaxPrice string `xml:"maxPrice"`
		} `xml:"contractInfo"`
		PurchaseObjectsInfo struct {
			TotalSum string `xml:"total"`
		} `xml:"purchaseObjectsInfo"`
	} `xml:"notificationInfo"`

	// 223-ФЗ варианты
	Body struct {
		Item struct {
			PurchaseNoticeData struct {
				RegistrationNumber string `xml:"registrationNumber"`
				Name               string `xml:"name"`
				CreateDateTime     string `xml:"createDateTime"`
				PublicationDateTime string `xml:"publicationDateTime"`
				ModificationDate   string `xml:"modificationDate"`
				Customer           struct {
					MainInfo struct {
						FullName string `xml:"fullName"`
						INN      string `xml:"inn"`
					} `xml:"mainInfo"`
				} `xml:"customer"`
				Lots struct {
					Lot []struct {
						LotData struct {
							InitialSum string `xml:"initialSum"`
						} `xml:"lotData"`
					} `xml:"lot"`
				} `xml:"lots"`
				PurchaseCodeName string `xml:"purchaseCodeName"`
			} `xml:"purchaseNoticeData"`
		} `xml:"item"`
	} `xml:"body"`
}

func (n notificationXML) toTender(law, docType string) (model.Tender, bool) {
	number := strings.TrimSpace(n.PurchaseNumber)
	desc := strings.TrimSpace(n.PurchaseObjectInfo)
	customer := strings.TrimSpace(n.PurchaseResponsible.ResponsibleOrg.FullName)
	inn := strings.TrimSpace(n.PurchaseResponsible.ResponsibleOrg.INN)

	if customer == "" {
		customer = strings.TrimSpace(n.Customer.FullName)
	}
	if inn == "" {
		inn = strings.TrimSpace(n.Customer.INN)
	}

	// 223 fallback
	pn := n.Body.Item.PurchaseNoticeData
	if number == "" {
		number = strings.TrimSpace(pn.RegistrationNumber)
	}
	if desc == "" {
		desc = strings.TrimSpace(pn.Name)
		if desc == "" {
			desc = strings.TrimSpace(pn.PurchaseCodeName)
		}
	}
	if customer == "" {
		customer = strings.TrimSpace(pn.Customer.MainInfo.FullName)
	}
	if inn == "" {
		inn = strings.TrimSpace(pn.Customer.MainInfo.INN)
	}

	if number == "" {
		return model.Tender{}, false
	}

	nmckStr := firstNonEmpty(
		n.Lot.MaxPrice,
		n.NotificationInfo.ContractInfo.MaxPrice,
		n.NotificationInfo.PurchaseObjectsInfo.TotalSum,
	)
	if nmckStr == "" && len(pn.Lots.Lot) > 0 {
		nmckStr = pn.Lots.Lot[0].LotData.InitialSum
	}

	var nmck *decimal.Decimal
	if nmckStr != "" {
		if d, err := decimal.NewFromString(normalizeNumber(nmckStr)); err == nil {
			nmck = &d
		}
	}

	endRaw := firstNonEmpty(
		n.NotificationInfo.ProcedureInfo.Collecting.EndDT,
		n.NotificationInfo.ProcedureInfo.Collecting.EndDate,
	)
	endDate := parseFlexibleTime(endRaw)

	updatedRaw := firstNonEmpty(
		n.ModificationDate,
		pn.ModificationDate,
		n.DocPublishDate,
		pn.PublicationDateTime,
		pn.CreateDateTime,
	)
	updated := parseFlexibleTime(updatedRaw)

	if law == "" {
		law = "44"
	}

	return model.Tender{
		PurchaseNumber: number,
		Description:    desc,
		Customer:       customer,
		CustomerINN:    inn,
		NMCK:           nmck,
		EndDate:        endDate,
		LastUpdatedAt:  updated,
		Law:            law,
		DocumentType:   docType,
		RawSource:      "eis",
	}, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func normalizeNumber(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	return s
}

func parseFlexibleTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05.000",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
		// без зоны — считаем Europe/Moscow приблизительно как +03
		if t, err := time.ParseInLocation(layout, s, time.FixedZone("MSK", 3*3600)); err == nil {
			return &t
		}
	}
	// иногда приходит unix?
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 1_000_000_000 {
		t := time.Unix(n, 0).UTC()
		return &t
	}
	return nil
}
