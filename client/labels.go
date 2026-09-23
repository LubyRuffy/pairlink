package client

import "github.com/LubyRuffy/pairlink/protocol"

// SetLabels stores this endpoint's name and model and announces them on the
// relay socket. An empty field does not clear a label the hub already has.
// Name and model are sent as separate fields.
func (c *Conn) SetLabels(name, model string) {
	if c == nil {
		return
	}
	name = protocol.SanitizeLabel(name)
	model = protocol.SanitizeLabel(model)
	c.mu.Lock()
	same := c.labelName == name && c.labelModel == model
	c.labelName = name
	c.labelModel = model
	c.mu.Unlock()
	if same || c.stopped() {
		return
	}
	c.announceLabels()
}

func (c *Conn) announceLabels() {
	if c == nil || c.stopped() {
		return
	}
	c.mu.Lock()
	name, model := c.labelName, c.labelModel
	c.mu.Unlock()
	payload, err := protocol.MarshalLabel(name, model)
	if err != nil {
		return
	}
	_ = c.writeWS(c.newFrame(protocol.TypeLabel, nil, payload))
}
