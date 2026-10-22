package pages

type profileAvatarOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type profileInterest struct {
	ID      string                `json:"id"`
	Label   string                `json:"label"`
	Avatars []profileAvatarOption `json:"avatars"`
}

var profileInterests = []profileInterest{
	{"gym", "Gym enthusiast", []profileAvatarOption{{"gym-female", "Gym, ponytail"}, {"gym-male", "Gym, short hair"}}},
	{"foodie", "Foodie", []profileAvatarOption{{"foodie-female", "Foodie, long hair"}, {"foodie-male", "Foodie, short hair"}}},
	{"student", "Student", []profileAvatarOption{{"student", "Student"}}},
	{"tech", "Tech enthusiast", []profileAvatarOption{{"tech-enthusiast", "Tech enthusiast"}}},
	{"gamer", "Gamer", []profileAvatarOption{{"gamer", "Gamer"}}},
	{"skincare", "Skincare lover", []profileAvatarOption{{"skincare-lover", "Skincare lover"}}},
	{"coffee", "Coffee lover", []profileAvatarOption{{"coffee-lover", "Coffee lover"}}},
	{"books", "Book lover", []profileAvatarOption{{"book-lover", "Book lover"}}},
	{"music", "Music lover", []profileAvatarOption{{"music-lover", "Music lover"}}},
	{"artist", "Artist", []profileAvatarOption{{"artist", "Artist"}}},
	{"sports", "Sports fan", []profileAvatarOption{{"sports-fan", "Sports fan"}}},
	{"fashion", "Fashion enthusiast", []profileAvatarOption{{"fashion-enthusiast", "Fashion enthusiast"}}},
	{"plants", "Plant lover", []profileAvatarOption{{"plant-lover", "Plant lover"}}},
	{"pets", "Pet lover", []profileAvatarOption{{"pet-lover", "Pet lover"}}},
	{"night-owl", "Night owl", []profileAvatarOption{{"night-owl", "Night owl"}}},
	{"everyday", "Everyday shopper", []profileAvatarOption{{"everyday-shopper", "Everyday shopper"}}},
	{"otaku", "Anime fan", []profileAvatarOption{{"otaku-fantasy", "Fantasy"}, {"otaku-shonen", "Shonen"}, {"otaku-magical-girl", "Magical girl"}, {"otaku-cosplayer", "Cosplayer"}}},
}

func profileAvatarURL(id string) string {
	for _, group := range profileInterests {
		for _, avatar := range group.Avatars {
			if avatar.ID == id {
				return "/static/images/avatars/" + id + ".png"
			}
		}
	}
	return "/static/images/avatars/everyday-shopper.png"
}
