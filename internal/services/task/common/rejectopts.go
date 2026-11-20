package common

import (
	"go.mongodb.org/mongo-driver/mongo/options"
)

func mongoFindOptions(limit int64) *options.FindOptions {
	return options.Find().SetSort(map[string]int{"at": -1}).SetLimit(limit)
}
