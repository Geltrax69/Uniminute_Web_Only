package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminReportDatesTotalsStoreIsolationAndExcel(t *testing.T) {
	f := securityFixture(t)
	admin := adminSignIn(t, f.h)
	for _, query := range []string{
		`UPDATE orders SET placed_at='2026-09-30T20:00:00Z',delivered_at='2026-10-02T06:00:00Z',stage='delivered',delivery_fee=15 WHERE id='security-a-order'`,
		`UPDATE orders SET placed_at='2026-10-01T06:00:00Z',stage='rejected',reject_reason='Kitchen closed' WHERE id='security-b-order'`,
		`INSERT INTO orders(id,item_id,item_title,units,amount,stage,store_owner,store_name,placed_at,delivered_at,delivery_fee) VALUES('older-delivered','security-a','Older item',1,55.50,'delivered','` + DefaultOwner + `','Store A','2026-09-28T00:00:00Z','2026-10-01T00:00:00Z',3)`,
		`INSERT INTO orders(id,item_id,item_title,units,amount,stage,store_owner,store_name,placed_at) VALUES('in-progress','security-a','In progress',1,50,'accepted','` + DefaultOwner + `','Store A','2026-10-01T12:00:00Z')`,
		`INSERT INTO order_reviews(order_id,buyer_email,item_id,item_rating,item_text,rider_rating,rider_text,updated_at) VALUES('security-a-order','buyer-a','security-a',5,'=HYPERLINK("https://evil.invalid","unsafe")',4,'Good delivery','2026-10-04T06:00:00Z')`,
	} {
		if _, err := f.db.sql.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("GET", "/api/admin/reports?month=2026-10&day=2026-10-01", nil)
	api := &API{db: f.db}
	report, err := api.loadAdminReport(req)
	if err != nil {
		t.Fatal(err)
	}
	if report.Monthly.Placed != 3 || report.Monthly.Accepted != 1 || report.Monthly.Rejected != 1 || report.Monthly.Delivered != 1 || report.Monthly.Sales != 155.50 || report.Monthly.Fees != 18 || report.Monthly.Reviewed != 1 || report.Monthly.DeliveryReviewed != 1 {
		t.Fatalf("month: %+v", report.Monthly)
	}
	if report.Daily.Sales != 55.50 || report.Daily.Placed != 3 || report.Daily.Reviewed != 0 {
		t.Fatalf("day: %+v", report.Daily)
	}
	filtered := rawAs(f.h, admin, "GET", "/api/admin/reports?month=2026-10&day=2026-10-01&store=seller-b%40example.test", "")
	if filtered.Code != 200 || strings.Contains(filtered.Body.String(), "Customer Alice") || strings.Contains(filtered.Body.String(), "Store A") {
		t.Fatalf("store filter failed: %d %s", filtered.Code, filtered.Body)
	}
	var got adminReport
	if json.Unmarshal(filtered.Body.Bytes(), &got) != nil || got.Monthly.Sales != 0 || got.Monthly.Rejected != 1 {
		t.Fatal("rejected order counted as revenue")
	}
	export := rawAs(f.h, admin, "GET", "/api/admin/reports/export?month=2026-10&day=2026-10-01", "")
	if export.Code != 200 || !strings.Contains(export.Header().Get("Content-Disposition"), ".xlsx") {
		t.Fatalf("export %d %s", export.Code, export.Body)
	}
	z, err := zip.NewReader(bytes.NewReader(export.Body.Bytes()), int64(export.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	sheets, unsafe, hasReview := 0, false, false
	for _, f := range z.File {
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(reader)
		reader.Close()
		decoder := xml.NewDecoder(bytes.NewReader(data))
		for {
			_, err = decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("invalid Excel XML %s: %v", f.Name, err)
			}
		}
		if strings.HasPrefix(f.Name, "xl/worksheets/") {
			sheets++
			unsafe = unsafe || bytes.Contains(data, []byte("<f>"))
			hasReview = hasReview || bytes.Contains(data, []byte("HYPERLINK"))
		}
	}
	if sheets != 6 || unsafe || !hasReview {
		t.Fatalf("Excel content: sheets=%d formula=%v review=%v", sheets, unsafe, hasReview)
	}
	if w := rawAs(f.h, admin, "GET", "/api/admin/reports?month=2026-10&day=2026-11-01", ""); w.Code != 400 {
		t.Fatal("cross-month day accepted")
	}
}
