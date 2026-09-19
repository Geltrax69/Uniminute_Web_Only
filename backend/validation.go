package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var indianPhone = regexp.MustCompile(`^(?:\+91[ -]?)?[6-9][0-9]{9}$`)
var postalCode = regexp.MustCompile(`^[1-9][0-9]{5}$`)

func textLimit(value, label string, maximum int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", label)
	}
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s must be at most %d characters", label, maximum)
	}
	if strings.ContainsAny(value, "<>") || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return fmt.Errorf("%s contains unsupported characters", label)
	}
	return nil
}

func validateAddress(in *Address) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Line = strings.TrimSpace(in.Line)
	in.Label = strings.TrimSpace(in.Label)
	in.Pincode = strings.TrimSpace(in.Pincode)
	for _, field := range []struct {
		value, label string
		max          int
		required     bool
	}{
		{in.Name, "recipient name", 100, false}, {in.Line, "street address", 300, true}, {in.Label, "address label", 40, false},
	} {
		if err := textLimit(field.value, field.label, field.max, field.required); err != nil {
			return err
		}
	}
	if in.Phone != "" && !indianPhone.MatchString(in.Phone) {
		return fmt.Errorf("enter a valid 10-digit Indian mobile number")
	}
	if in.Phone != "" {
		in.Phone = normalisePhone(in.Phone)
	}
	if in.Pincode != "" && !postalCode.MatchString(in.Pincode) {
		return fmt.Errorf("enter a valid 6-digit pincode")
	}
	return nil
}

// plainLines turns CRLF and lone CR line breaks into "\n". Browsers send a
// textarea's line breaks as CRLF in multipart forms, and a bare "\r" would
// otherwise be refused as an unsupported character.
func plainLines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// maxItemTitle fits a full marketplace-style name (brand, model, key specs).
// Mirrored in the seller form (seller-product.templ) so it is never a surprise.
const maxItemTitle = 250

func (a *API) validateItem(ctx context.Context, in *InventoryItem) error {
	in.Title = strings.TrimSpace(plainLines(in.Title))
	in.Description = plainLines(in.Description)
	in.Category = strings.TrimSpace(in.Category)
	if err := textLimit(in.Title, "title", maxItemTitle, true); err != nil {
		return err
	}
	if err := textLimit(in.Description, "description", 5000, false); err != nil {
		return err
	}
	if !isFinitePrice(in.Price) || in.Price <= 0 {
		return fmt.Errorf("price must be between ₹0.01 and ₹999999.99")
	}
	if !isFinitePrice(in.MRP) || in.MRP < 0 {
		return fmt.Errorf("MRP must be between ₹0 and ₹999999.99")
	}
	if in.Stock < 0 || in.Stock > 1000000 {
		return fmt.Errorf("stock must be between 0 and 1000000")
	}
	if in.Category != "" {
		var exists bool
		if err := a.db.sql.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_categories WHERE name=$1)`, in.Category).Scan(&exists); err != nil {
			return fmt.Errorf("could not validate category — try again")
		}
		if !exists {
			return fmt.Errorf("choose an existing category")
		}
	}
	if err := validateVariantPrices(in); err != nil {
		return err
	}
	return nil
}
func isFinitePrice(n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) && n <= 999999.99 && math.Abs(n*100-math.Round(n*100)) < 0.00001
}
