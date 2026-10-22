package main

// Avatar identifiers are an allow-list, never user supplied URLs or paths.
var profileAvatars = map[string]string{
	"gym-female": "gym", "gym-male": "gym", "foodie-female": "foodie", "foodie-male": "foodie",
	"student": "student", "tech-enthusiast": "tech", "gamer": "gamer", "skincare-lover": "skincare",
	"coffee-lover": "coffee", "book-lover": "books", "music-lover": "music", "artist": "artist",
	"sports-fan": "sports", "fashion-enthusiast": "fashion", "plant-lover": "plants", "pet-lover": "pets",
	"night-owl": "night-owl", "everyday-shopper": "everyday", "otaku-fantasy": "otaku", "otaku-shonen": "otaku",
	"otaku-magical-girl": "otaku", "otaku-cosplayer": "otaku",
}
