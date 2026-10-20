package classification

type Input struct {
	ID         string `json:"id"`
	Store      string `json:"store"`
	Title      string `json:"name"`
	Category   string `json:"category"`
	Department string `json:"tab"`
}

type Issue struct {
	ID             string `json:"id"`
	Store          string `json:"store"`
	Title          string `json:"title"`
	Category       string `json:"category"`
	Classification Result `json:"classification"`
}

type Report struct {
	Version    string         `json:"version"`
	Products   int            `json:"products"`
	Groups     map[string]int `json:"groups"`
	Review     []Issue        `json:"review"`
	Duplicates [][]Issue      `json:"duplicates"`
}

func Audit(products []Input) Report {
	report := Report{Version: Version, Products: len(products), Groups: map[string]int{}, Review: []Issue{}, Duplicates: [][]Issue{}}
	index := map[string]int{}
	var sets [][]Issue
	for _, p := range products {
		result := Classify(p.Department, p.Category, p.Title)
		item := Issue{p.ID, p.Store, p.Title, p.Category, result}
		report.Groups[p.Department+" / "+result.Group]++
		if result.NeedsReview {
			report.Review = append(report.Review, item)
		}
		key := Normalize(p.Store) + "\x00" + Normalize(p.Title)
		i, exists := index[key]
		if !exists {
			i = len(sets)
			index[key] = i
			sets = append(sets, nil)
		}
		sets[i] = append(sets[i], item)
	}
	for _, set := range sets {
		if len(set) > 1 {
			report.Duplicates = append(report.Duplicates, set)
		}
	}
	return report
}
