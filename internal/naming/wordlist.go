package naming

import (
	"math/rand"
	"time"
)

var adjectives = []string{
	// Nature & weather
	"Amber", "Arctic", "Ashen", "Autumn", "Azure",
	"Blazing", "Bright", "Bronze", "Calm", "Cedar",
	"Cobalt", "Copper", "Coral", "Crimson", "Crystal",
	"Dawn", "Deep", "Dusk", "Dusty", "Ember",
	"Fern", "Flint", "Frosty", "Golden", "Granite",
	// Character
	"Bold", "Brave", "Clear", "Clever", "Deft",
	"Eager", "Fair", "Fierce", "Fleet", "Grand",
	"Hardy", "Keen", "Noble", "Prime", "Proud",
	"Quick", "Sharp", "Steady", "Swift", "True",
	// Atmosphere
	"Cool", "Dusky", "Fresh", "Gentle", "Hushed",
	"Iron", "Lunar", "Misty", "Mossy", "Quiet",
	"Rustic", "Sandy", "Silver", "Solar", "Stone",
	"Tidal", "Vivid", "Warm", "Wild", "Woven",
}

var nouns = []string{
	// Landscape
	"Anchor", "Basin", "Bluff", "Canyon", "Cliff",
	"Coast", "Cove", "Crest", "Delta", "Dune",
	"Fjord", "Glade", "Glen", "Grove", "Haven",
	"Hollow", "Isle", "Ledge", "Mesa", "Moor",
	"Oasis", "Pass", "Peak", "Perch", "Plateau",
	"Ravine", "Reef", "Ridge", "Shore", "Summit",
	// Structure & craft
	"Anvil", "Arch", "Arrow", "Atlas", "Bastion",
	"Beacon", "Bridge", "Cairn", "Citadel", "Compass",
	"Crown", "Ember", "Flare", "Forge", "Gate",
	"Harbor", "Helm", "Keep", "Lantern", "Loom",
	"Mantle", "Marker", "Mast", "Outpost", "Pinnacle",
	"Prism", "Quarry", "Rampart", "Sentry", "Shrine",
	"Signal", "Spark", "Spire", "Torch", "Vault",
	"Watch", "Wharf",
}

// WordlistName generates a memorable adjective+noun name.
// 60 adjectives x 47 nouns = 2,820 unique combinations.
func WordlistName() string {
	src := rand.New(rand.NewSource(time.Now().UnixNano()))
	adj := adjectives[src.Intn(len(adjectives))]
	noun := nouns[src.Intn(len(nouns))]
	return adj + noun
}
