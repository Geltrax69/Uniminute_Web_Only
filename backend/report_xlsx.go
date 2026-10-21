package main

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"time"
)

type moneyCell float64
type workbookSheet struct {
	Name string
	Rows [][]any
}

func reportWorkbook(r adminReport) ([]byte, error) {
	metrics := func(m reportMetrics) []any {
		return []any{m.Placed, m.Accepted, m.Rejected, m.Delivered, m.Pending, m.Reviewed, m.DeliveryReviewed, moneyCell(m.Sales), moneyCell(m.Fees)}
	}
	header := []any{"Date (IST)", "Store", "Store owner", "Orders placed", "Accepted / picked", "Rejected", "Delivered orders", "Awaiting acceptance", "Product reviews updated", "Delivery reviews updated", "Delivered item sales (INR)", "Delivered delivery fees (INR)"}
	summary := [][]any{{"Uniminute operations report", r.Month}, {"Timezone", "Asia/Kolkata"}, {"Selected day", r.Day}, {"Store filter", r.Store}, {"Metric", "Selected day", "Month"}}
	names := []string{"Orders placed", "Accepted / picked", "Rejected", "Delivered orders", "Awaiting acceptance", "Product reviews updated", "Delivery reviews updated", "Delivered item sales (INR)", "Delivered delivery fees (INR)"}
	day, month := metrics(r.Daily), metrics(r.Monthly)
	for i, name := range names {
		summary = append(summary, []any{name, day[i], month[i]})
	}
	summary = append(summary, []any{"Legacy delivery date fallbacks", r.LegacyDates}, []any{"Sales definition", "Delivered product amount before refunds; excludes delivery fees and is not profit."}, []any{"Order counts", "Each product/option order line is one order record. A basket may create several records."}, []any{"Status counts", "Current status of orders placed in the period. Accepted/picked excludes delivered and rejected."}, []any{"Sales dates", "Delivery date in IST; records without a delivery timestamp use placement date."}, []any{"Review dates", "Last review update in IST; product and delivery reviews counted separately."}, []any{"Order detail scope", "Orders placed, delivered or reviewed in this month; use timestamps for reconciliation."}, []any{"Privacy", "Admin-only customer details. Store this export securely and delete it when no longer needed."})
	daily := [][]any{header}
	stores := [][]any{header}
	for _, row := range r.Days {
		daily = append(daily, append([]any{row.Date, row.Store, row.Owner}, metrics(row.reportMetrics)...))
	}
	for _, row := range r.Stores {
		stores = append(stores, append([]any{r.Month, row.Store, row.Owner}, metrics(row.reportMetrics)...))
	}
	orders := [][]any{{"Order reference", "Internal order ID", "Store", "Store owner", "Item", "Units", "Item amount (INR)", "Delivery fee (INR)", "Current status", "Placed at (IST)", "Delivered at (IST)", "Payment method", "Payment status", "Rejection reason", "Customer", "Customer phone", "Delivery address", "Buyer email", "Assigned rider", "Delivery rider"}}
	rejections := [][]any{orders[0]}
	reviews := [][]any{{"Order reference", "Store", "Item", "Product stars", "Product review", "Delivery stars", "Delivery review", "Delivery rider", "Updated at (IST)"}}
	for _, o := range r.Orders {
		delivered := ""
		if o.DeliveredAt != nil {
			delivered = reportTime(*o.DeliveredAt)
		}
		row := []any{orderReference(o.ID), o.ID, o.StoreName, o.StoreOwner, o.ItemTitle, o.Units, moneyCell(o.Amount), moneyCell(o.DeliveryFee), string(o.Stage), reportTime(o.PlacedAt), delivered, o.PaymentMethod, o.PaymentStatus, o.RejectReason, o.ReceiverName, o.ReceiverPhone, o.ReceiverAddress, o.BuyerEmail, o.AssignedTo, o.RiderPhone}
		orders = append(orders, row)
		if o.Stage == "rejected" {
			rejections = append(rejections, row)
		}
		if o.ReviewAt != nil {
			reviews = append(reviews, []any{orderReference(o.ID), o.StoreName, o.ItemTitle, o.ItemRating, o.ItemText, o.RiderRating, o.RiderText, o.RiderPhone, reportTime(*o.ReviewAt)})
		}
	}
	return writeWorkbook([]workbookSheet{{"Summary", summary}, {"Store totals", stores}, {"Daily stores", daily}, {"Orders", orders}, {"Rejected orders", rejections}, {"Reviews", reviews}})
}
func reportTime(t time.Time) string { return t.In(reportIST).Format("2006-01-02 15:04:05") }
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func columnName(index int) string {
	out := ""
	for index > 0 {
		index--
		out = string(rune('A'+index%26)) + out
		index /= 26
	}
	return out
}

// Native OOXML export has no macros, formulas, external links, or shared-string
// evaluation. Every user-controlled value is an escaped inline string, even if
// it starts with =, +, -, or @. Numeric/currency cells stay numeric for Excel.
func writeWorkbook(sheets []workbookSheet) ([]byte, error) {
	var output bytes.Buffer
	z := zip.NewWriter(&output)
	add := func(name, body string) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	types := `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	wb := `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`
	rels := `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`
	for i, s := range sheets {
		n := strconv.Itoa(i + 1)
		types += `<Override PartName="/xl/worksheets/sheet` + n + `.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`
		wb += `<sheet name="` + xmlText(s.Name) + `" sheetId="` + n + `" r:id="rId` + n + `"/>`
		rels += `<Relationship Id="rId` + n + `" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet` + n + `.xml"/>`
		f, err := z.Create("xl/worksheets/sheet" + n + ".xml")
		if err != nil {
			return nil, err
		}
		io.WriteString(f, `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="3" width="24" customWidth="1"/><col min="4" max="20" width="22" customWidth="1"/></cols><sheetData>`)
		for ri, row := range s.Rows {
			fmt.Fprintf(f, `<row r="%d">`, ri+1)
			for ci, value := range row {
				ref := columnName(ci+1) + strconv.Itoa(ri+1)
				style := 0
				if ri == 0 {
					style = 1
				}
				switch v := value.(type) {
				case moneyCell:
					fmt.Fprintf(f, `<c r="%s" s="2"><v>%.2f</v></c>`, ref, v)
				case int:
					fmt.Fprintf(f, `<c r="%s" s="%d"><v>%d</v></c>`, ref, style, v)
				default:
					fmt.Fprintf(f, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, xmlText(fmt.Sprint(value)))
				}
			}
			io.WriteString(f, `</row>`)
		}
		io.WriteString(f, `</sheetData>`)
		if len(s.Rows) > 1 && len(s.Rows[0]) > 3 {
			fmt.Fprintf(f, `<autoFilter ref="A1:%s%d"/>`, columnName(len(s.Rows[0])), len(s.Rows))
		}
		if _, err = io.WriteString(f, `</worksheet>`); err != nil {
			return nil, err
		}
	}
	types += `</Types>`
	wb += `</sheets></workbook>`
	rels += `<Relationship Id="styles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
	for _, file := range []struct{ name, body string }{{"[Content_Types].xml", types}, {"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="workbook" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`}, {"xl/workbook.xml", wb}, {"xl/_rels/workbook.xml.rels", rels}, {"xl/styles.xml", `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="&quot;₹&quot;#,##0.00"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><color rgb="FFFFFFFF"/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF1D4939"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="1"><border/></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="3"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0"/><xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`}} {
		if err := add(file.name, file.body); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
