package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Choice is one option the buyer picked, e.g. {Colour, Black}.
type Choice struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Choices is stored as JSONB on the order.
type Choices []Choice

func (c *Choices) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	case nil:
		*c = Choices{}
		return nil
	default:
		return fmt.Errorf("choices: unexpected %T", src)
	}
	out := Choices{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*c = out
	return nil
}

func (c Choices) key() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// matchChoices checks the buyer picked exactly one offered value for every
// option group the item has, and nothing else. It answers them in the item's
// own order.
func matchChoices(title string, offered []ItemOption, picked Choices) (Choices, error) {
	got := map[string]string{}
	for _, c := range picked {
		if _, exists := got[c.Name]; exists {
			return nil, fmt.Errorf("choose each option only once")
		}
		got[c.Name] = c.Value
	}
	out := Choices{}
	for _, o := range offered {
		if len(o.Values) == 0 {
			continue
		}
		v, ok := got[o.Name]
		if !ok || strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("choose %s for %s", strings.ToLower(o.Name), title)
		}
		valid := false
		for _, allowed := range o.Values {
			valid = valid || allowed == v
		}
		if !valid {
			return nil, fmt.Errorf("%s is not offered for %s", v, title)
		}
		out = append(out, Choice{o.Name, v})
		delete(got, o.Name)
	}
	for name := range got {
		return nil, fmt.Errorf("%s has no %s option", title, strings.ToLower(name))
	}
	return out, nil
}
