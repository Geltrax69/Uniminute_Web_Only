package pages

import (
	"image/png"
	"lamazon/website/static"
	"strings"
	"testing"
)

func TestProfileAvatarChoicesHaveShippedImages(t *testing.T) {
	seen := map[string]bool{}
	for _, group := range profileInterests {
		if len(group.Avatars) == 0 {
			t.Fatalf("empty interest %s", group.ID)
		}
		for _, avatar := range group.Avatars {
			if seen[avatar.ID] {
				t.Fatalf("ambiguous avatar %s", avatar.ID)
			}
			seen[avatar.ID] = true
			file, err := static.Files.Open(strings.TrimPrefix(profileAvatarURL(avatar.ID), "/static/"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := png.DecodeConfig(file)
			file.Close()
			if err != nil || cfg.Width != 400 || cfg.Height != 400 {
				t.Fatalf("invalid avatar image %s", avatar.ID)
			}
		}
	}
}
