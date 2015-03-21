package gone

import (
	"gopkg.in/mgo.v2/bson"
)

type gonePayload struct {
	Data []byte
	Id   string
}

func encodePayload(payload *gonePayload) ([]byte, error) {
	return bson.Marshal(payload)
}

func decodePayload(data []byte) (*gonePayload, error) {
	payload := gonePayload{}
	err := bson.Unmarshal(data, &payload)
	return &payload, err
}
