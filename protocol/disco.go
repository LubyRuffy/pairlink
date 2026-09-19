package protocol

import "encoding/json"

// Disco is endpoint advertisement. It is sealed inside TypeDisco so the hub
// cannot forge candidates; the hub still learns addresses it observed itself
// via TypeObserved.
type Disco struct {
	Endpoints []string `json:"endpoints"`
	CallMe    bool     `json:"call_me"`
}

func (d Disco) Marshal() ([]byte, error) {
	return json.Marshal(d)
}

func UnmarshalDisco(b []byte) (Disco, error) {
	var d Disco
	err := json.Unmarshal(b, &d)
	return d, err
}

// Observed is injected by the hub from the transport peer address. Not an
// application payload.
type Observed struct {
	Addr string `json:"addr"`
	Via  string `json:"via"` // "tcp" or "udp"
}

func (o Observed) Marshal() ([]byte, error) {
	return json.Marshal(o)
}

func UnmarshalObserved(b []byte) (Observed, error) {
	var o Observed
	err := json.Unmarshal(b, &o)
	return o, err
}
