package eis_test

import (
	"testing"
	"time"

	"github.com/rinat1313/zakupki-collector/internal/eis"
)

const sampleNotification = `<?xml version="1.0" encoding="UTF-8"?>
<data>
  <fcsNotificationEF>
    <purchaseNumber>0373100010025000001</purchaseNumber>
    <docPublishDate>2026-08-15T10:00:00</docPublishDate>
    <modificationDate>2026-08-15T10:15:00</modificationDate>
    <purchaseObjectInfo>Поставка бумаги для офиса</purchaseObjectInfo>
    <purchaseResponsible>
      <responsibleOrg>
        <fullName>ГБУ Тест</fullName>
        <INN>7701234567</INN>
      </responsibleOrg>
    </purchaseResponsible>
    <lot>
      <maxPrice>150000.50</maxPrice>
    </lot>
    <notificationInfo>
      <procedureInfo>
        <collecting>
          <endDate>2026-08-20T12:00:00</endDate>
        </collecting>
      </procedureInfo>
    </notificationInfo>
  </fcsNotificationEF>
</data>`

func TestParseArchives_Notification(t *testing.T) {
	since := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	items, err := eis.ParseArchives([]byte(sampleNotification), "44", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 tender, got %d", len(items))
	}
	tr := items[0]
	if tr.PurchaseNumber != "0373100010025000001" {
		t.Errorf("number=%q", tr.PurchaseNumber)
	}
	if tr.Description != "Поставка бумаги для офиса" {
		t.Errorf("desc=%q", tr.Description)
	}
	if tr.Customer != "ГБУ Тест" || tr.CustomerINN != "7701234567" {
		t.Errorf("customer=%q inn=%q", tr.Customer, tr.CustomerINN)
	}
	if tr.NMCK == nil || tr.NMCK.String() != "150000.5" {
		t.Errorf("nmck=%v", tr.NMCK)
	}
	if tr.LastUpdatedAt == nil {
		t.Fatal("last_updated_at is nil")
	}
}

func TestParseArchives_SkipsOld(t *testing.T) {
	// modificationDate в XML без зоны парсится как UTC → порог тоже в UTC.
	since := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	items, err := eis.ParseArchives([]byte(sampleNotification), "44", since)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected skip old, got %d", len(items))
	}
}

func TestParseDocsResponse_NoData(t *testing.T) {
	// covered indirectly via client package — ensure parse does not panic on empty
	items, err := eis.ParseArchives([]byte(`<root></root>`), "44", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("want 0, got %d", len(items))
	}
}
