package bond

import (
	"encoding/json"

	"github.com/tychoish/grip"
)

// BuildTypes represents all information about builds in a cache.
type BuildTypes struct {
	Version       string           `bson:"version" json:"version" yaml:"version"`
	Targets       []string         `bson:"targets" json:"targets" yaml:"targets"`
	Editions      []MongoDBEdition `bson:"editions" json:"editions" yaml:"editions"`
	Architectures []MongoDBArch    `bson:"architectures" json:"architectures" yaml:"architectures"`
}

func (b BuildTypes) String() string {
	out, err := json.MarshalIndent(b, "   ", "   ")
	grip.CatchError(err)

	return string(out) + "\n"
}
