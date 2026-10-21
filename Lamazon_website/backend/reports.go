package backend

import "time"

type ReportMetrics struct {
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
type ReportRow struct {
	Date  string `json:"date"`
	Store string `json:"store"`
	Owner string `json:"owner"`
	ReportMetrics
}
type ReportOrder struct {
	Order
	ItemRating  int        `json:"itemRating"`
	ItemText    string     `json:"itemText"`
	RiderRating int        `json:"riderRating"`
	RiderText   string     `json:"riderText"`
	ReviewAt    *time.Time `json:"reviewAt"`
}
type AdminReport struct {
	Month          string        `json:"month"`
	Day            string        `json:"day"`
	Store          string        `json:"store"`
	Timezone       string        `json:"timezone"`
	Daily          ReportMetrics `json:"daily"`
	Monthly        ReportMetrics `json:"monthly"`
	Days           []ReportRow   `json:"days"`
	Stores         []ReportRow   `json:"stores"`
	Orders         []ReportOrder `json:"orders"`
	LegacyDates    int           `json:"legacyDates"`
	DetailsLimited bool          `json:"detailsLimited"`
}
