package main

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

var reportIST = time.FixedZone("IST", 5*3600+30*60)

type reportMetrics struct {
	Placed           int     `json:"placed"`
	Accepted         int     `json:"accepted"`
	Rejected         int     `json:"rejected"`
	Delivered        int     `json:"delivered"`
	Pending          int     `json:"pending"`
	Reviewed         int     `json:"reviewed"`
	DeliveryReviewed int     `json:"deliveryReviewed"`
	Sales            float64 `json:"sales"`
	Fees             float64 `json:"fees"`
}
type reportRow struct {
	Date  string `json:"date"`
	Store string `json:"store"`
	Owner string `json:"owner"`
	reportMetrics
}
type reportOrder struct {
	Order
	ItemRating  int        `json:"itemRating"`
	ItemText    string     `json:"itemText"`
	RiderRating int        `json:"riderRating"`
	RiderText   string     `json:"riderText"`
	ReviewAt    *time.Time `json:"reviewAt"`
}
type adminReport struct {
	Month          string        `json:"month"`
	Day            string        `json:"day"`
	Store          string        `json:"store"`
	Timezone       string        `json:"timezone"`
	Daily          reportMetrics `json:"daily"`
	Monthly        reportMetrics `json:"monthly"`
	Days           []reportRow   `json:"days"`
	Stores         []reportRow   `json:"stores"`
	Orders         []reportOrder `json:"orders"`
	LegacyDates    int           `json:"legacyDates"`
	DetailsLimited bool          `json:"detailsLimited"`
}

func reportPeriod(r *http.Request) (time.Time, time.Time, string, error) {
	month := r.URL.Query().Get("month")
	if month == "" {
		month = time.Now().In(reportIST).Format("2006-01")
	}
	start, err := time.ParseInLocation("2006-01", month, reportIST)
	if err != nil {
		return start, start, "", fmt.Errorf("choose a valid report month")
	}
	day := r.URL.Query().Get("day")
	if day == "" {
		day = time.Now().In(reportIST).Format("2006-01-02")
		if !strings.HasPrefix(day, month) {
			day = month + "-01"
		}
	}
	parsed, err := time.ParseInLocation("2006-01-02", day, reportIST)
	if err != nil || parsed.Before(start) || !parsed.Before(start.AddDate(0, 1, 0)) {
		return start, start, "", fmt.Errorf("choose a day inside the selected month")
	}
	return start, start.AddDate(0, 1, 0), day, nil
}
func addReport(a *reportMetrics, b reportMetrics) {
	a.Placed += b.Placed
	a.Accepted += b.Accepted
	a.Rejected += b.Rejected
	a.Delivered += b.Delivered
	a.Pending += b.Pending
	a.Reviewed += b.Reviewed
	a.DeliveryReviewed += b.DeliveryReviewed
	a.Sales = math.Round((a.Sales+b.Sales)*100) / 100
	a.Fees = math.Round((a.Fees+b.Fees)*100) / 100
}

// One database snapshot feeds every view/export. Counts are order-line records
// (the application's order unit); a multi-item basket has several such records.
// Sales use delivery date, fees stay separate, reviews use last-submitted date.
func (a *API) loadAdminReport(r *http.Request) (adminReport, error) {
	start, end, day, err := reportPeriod(r)
	result := adminReport{Month: start.Format("2006-01"), Day: day, Store: r.URL.Query().Get("store"), Timezone: "Asia/Kolkata", Days: []reportRow{}, Stores: []reportRow{}, Orders: []reportOrder{}}
	if err != nil {
		return result, err
	}
	rows, err := a.db.sql.QueryContext(r.Context(), `SELECT o.id,o.item_id,o.item_title,o.units,o.amount,o.delivery_fee,o.stage,o.placed_at,o.delivered_at,
 o.store_owner,o.store_name,o.buyer_email,o.receiver_name,o.receiver_phone,o.receiver_address,o.reject_reason,o.rider_phone,o.assigned_to,o.payment_method,o.payment_status,
 COALESCE(v.item_rating,0),COALESCE(v.item_text,''),COALESCE(v.rider_rating,0),COALESCE(v.rider_text,''),v.updated_at
 FROM orders o LEFT JOIN order_reviews v ON v.order_id=o.id
 WHERE ($3::text='' OR o.store_owner=$3) AND
 ((o.placed_at >=$1 AND o.placed_at<$2) OR
 (o.stage='delivered' AND COALESCE(o.delivered_at,o.placed_at)>=$1 AND COALESCE(o.delivered_at,o.placed_at)<$2) OR
 (v.updated_at >=$1 AND v.updated_at<$2)) ORDER BY o.placed_at DESC,o.id LIMIT 50001`, start, end, result.Store)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	daily := map[string]*reportRow{}
	stores := map[string]*reportRow{}
	emit := func(o reportOrder, at time.Time, m reportMetrics) {
		if at.Before(start) || !at.Before(end) {
			return
		}
		date := at.In(reportIST).Format("2006-01-02")
		key := date + "\x00" + o.StoreOwner
		if daily[key] == nil {
			daily[key] = &reportRow{Date: date, Owner: o.StoreOwner, Store: o.StoreName}
		}
		if stores[o.StoreOwner] == nil {
			stores[o.StoreOwner] = &reportRow{Owner: o.StoreOwner, Store: o.StoreName}
		}
		addReport(&daily[key].reportMetrics, m)
		addReport(&stores[o.StoreOwner].reportMetrics, m)
		addReport(&result.Monthly, m)
		if date == day {
			addReport(&result.Daily, m)
		}
	}
	for rows.Next() {
		var o reportOrder
		if err = rows.Scan(&o.ID, &o.ItemID, &o.ItemTitle, &o.Units, &o.Amount, &o.DeliveryFee, &o.Stage, &o.PlacedAt, &o.DeliveredAt, &o.StoreOwner, &o.StoreName, &o.BuyerEmail, &o.ReceiverName, &o.ReceiverPhone, &o.ReceiverAddress, &o.RejectReason, &o.RiderPhone, &o.AssignedTo, &o.PaymentMethod, &o.PaymentStatus, &o.ItemRating, &o.ItemText, &o.RiderRating, &o.RiderText, &o.ReviewAt); err != nil {
			return result, err
		}
		if len(result.Orders) >= 50000 {
			return result, fmt.Errorf("this report has too many records; select one store to export")
		}
		result.Orders = append(result.Orders, o)
		m := reportMetrics{Placed: 1}
		switch o.Stage {
		case "received":
			m.Pending = 1
		case "accepted", "picked":
			m.Accepted = 1
		case "delivered":
			m.Delivered = 1
		case "rejected":
			m.Rejected = 1
		}
		emit(o, o.PlacedAt, m)
		if o.Stage == "delivered" {
			at := o.PlacedAt
			if o.DeliveredAt != nil {
				at = *o.DeliveredAt
			} else {
				result.LegacyDates++
			}
			emit(o, at, reportMetrics{Sales: o.Amount, Fees: o.DeliveryFee})
		}
		if o.ReviewAt != nil {
			v := reportMetrics{}
			if o.ItemRating > 0 {
				v.Reviewed = 1
			}
			if o.RiderRating > 0 {
				v.DeliveryReviewed = 1
			}
			emit(o, *o.ReviewAt, v)
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	for _, row := range daily {
		result.Days = append(result.Days, *row)
	}
	for _, row := range stores {
		result.Stores = append(result.Stores, *row)
	}
	sort.Slice(result.Days, func(i, j int) bool {
		if result.Days[i].Date != result.Days[j].Date {
			return result.Days[i].Date > result.Days[j].Date
		}
		if result.Days[i].Store != result.Days[j].Store {
			return result.Days[i].Store < result.Days[j].Store
		}
		return result.Days[i].Owner < result.Days[j].Owner
	})
	sort.Slice(result.Stores, func(i, j int) bool {
		if result.Stores[i].Sales != result.Stores[j].Sales {
			return result.Stores[i].Sales > result.Stores[j].Sales
		}
		return result.Stores[i].Owner < result.Stores[j].Owner
	})
	return result, nil
}
func (a *API) handleAdminReport(w http.ResponseWriter, r *http.Request) {
	report, err := a.loadAdminReport(r)
	if err != nil {
		if _, _, _, periodErr := reportPeriod(r); periodErr != nil {
			writeError(w, 400, periodErr.Error())
		} else {
			writeError(w, 503, "could not load the report; retry or choose one store")
		}
		return
	}
	// UI previews are bounded; monthly aggregates always include all records.
	if len(report.Orders) > 500 {
		report.Orders = report.Orders[:500]
		report.DetailsLimited = true
	}
	writeJSON(w, 200, report)
}
func (a *API) handleAdminReportExport(w http.ResponseWriter, r *http.Request) {
	if !a.allowPaymentRequests(w, r, "report-export:"+a.staffOf(r), 5, 60) {
		return
	}
	report, err := a.loadAdminReport(r)
	if err != nil {
		writeError(w, 400, "could not export report; check the month and select one store for large reports")
		return
	}
	data, err := reportWorkbook(report)
	if err != nil {
		writeError(w, 503, "could not build Excel report")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="uniminute-`+report.Month+`.xlsx"`)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(data)
}
