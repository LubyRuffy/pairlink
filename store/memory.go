package store

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// Memory is the in-process store used by tests and pairlinkd.
type Memory struct {
	mu       sync.Mutex
	hosts    []Host
	pairings []Pairing
	bindings []Binding
	trace    []TraceEvent
}

func NewMemory() *Memory { return &Memory{} }

func (m *Memory) PutHost(_ context.Context, h Host) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, err := locateHost(m.hosts, h)
	if err != nil {
		return err
	}
	if i >= 0 {
		m.hosts[i] = mergeHost(m.hosts[i], h)
		return nil
	}
	m.hosts = append(m.hosts, h)
	return nil
}

func (m *Memory) HostByTokenHash(_ context.Context, hash []byte) (Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.hosts {
		if bytes.Equal(h.TokenHash, hash) {
			return h, nil
		}
	}
	return Host{}, ErrNotFound
}

func (m *Memory) HostByPub(_ context.Context, pub []byte) (Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, h := range m.hosts {
		if bytes.Equal(h.Pub, pub) {
			return h, nil
		}
	}
	return Host{}, ErrNotFound
}

func (m *Memory) ListHosts(_ context.Context) ([]Host, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Host(nil), m.hosts...), nil
}

func (m *Memory) PutPairing(_ context.Context, p Pairing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pairings = append(m.pairings, p)
	return nil
}

func (m *Memory) LookupPairing(_ context.Context, codeHash []byte) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return inspectPairing(m.pairings, codeHash, time.Now())
}

func (m *Memory) ConsumePairing(_ context.Context, codeHash []byte) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	p, err := inspectPairing(m.pairings, codeHash, now)
	if err != nil {
		return Pairing{}, err
	}
	for i := range m.pairings {
		if bytes.Equal(m.pairings[i].CodeHash, codeHash) {
			m.pairings[i].Consumed = true
			p.Consumed = true
			return p, nil
		}
	}
	return Pairing{}, ErrNotFound
}

func (m *Memory) RestorePairing(_ context.Context, codeHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.pairings {
		if !bytes.Equal(m.pairings[i].CodeHash, codeHash) {
			continue
		}
		if !m.pairings[i].Consumed {
			return ErrNotFound
		}
		m.pairings[i].Consumed = false
		return nil
	}
	return ErrNotFound
}

func inspectPairing(rows []Pairing, codeHash []byte, now time.Time) (Pairing, error) {
	for _, p := range rows {
		if !bytes.Equal(p.CodeHash, codeHash) {
			continue
		}
		if p.Consumed {
			return Pairing{}, ErrConsumed
		}
		if now.After(p.Expires) {
			return Pairing{}, ErrExpired
		}
		return p, nil
	}
	return Pairing{}, ErrNotFound
}

func (m *Memory) PairingByID(_ context.Context, id string) (Pairing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pairings {
		if p.ID == id {
			return p, nil
		}
	}
	return Pairing{}, ErrNotFound
}

func (m *Memory) PutBinding(_ context.Context, b Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, x := range m.bindings {
		if bytes.Equal(x.HostPub, b.HostPub) && !x.Revoked && !bytes.Equal(x.DevicePub, b.DevicePub) {
			n++
		}
	}
	if n >= MaxBindingsPerHost {
		return ErrLimit
	}
	for i := range m.bindings {
		if bytes.Equal(m.bindings[i].HostPub, b.HostPub) && bytes.Equal(m.bindings[i].DevicePub, b.DevicePub) && !m.bindings[i].Revoked {
			m.bindings[i].Revoked = true
		}
	}
	m.bindings = append(m.bindings, b)
	return nil
}

func (m *Memory) BindingByTicketHash(_ context.Context, hash []byte) (Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.bindings {
		if bytes.Equal(b.TicketHash, hash) {
			if b.Revoked {
				return Binding{}, ErrRevoked
			}
			return b, nil
		}
	}
	return Binding{}, ErrNotFound
}

func (m *Memory) BindingByPeers(_ context.Context, hostPub, devicePub []byte) (Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return selectBinding(m.bindings, hostPub, devicePub)
}

func (m *Memory) ListBindings(_ context.Context, hostPub []byte) ([]Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Binding
	for _, b := range m.bindings {
		if bytes.Equal(b.HostPub, hostPub) && !b.Revoked {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *Memory) ListAllBindings(_ context.Context) ([]Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Binding(nil), m.bindings...), nil
}

func (m *Memory) RevokeBinding(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, b := range m.bindings {
		if b.ID == id {
			m.bindings[i].Revoked = true
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) NoteDeviceSeen(_ context.Context, devicePub []byte, at time.Time) error {
	if len(devicePub) != 32 || at.IsZero() {
		return nil
	}
	at = at.UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, b := range m.bindings {
		if bytes.Equal(b.DevicePub, devicePub) && (b.LastConnected.IsZero() || at.After(b.LastConnected)) {
			m.bindings[i].LastConnected = at
		}
	}
	return nil
}

func (m *Memory) SetDeviceLabels(_ context.Context, devicePub []byte, name, model, version string) error {
	if len(devicePub) != 32 || (name == "" && model == "" && version == "") {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.bindings {
		if !bytes.Equal(m.bindings[i].DevicePub, devicePub) {
			continue
		}
		if name != "" {
			m.bindings[i].DeviceName = name
		}
		if model != "" {
			m.bindings[i].DeviceModel = model
		}
		if version != "" {
			m.bindings[i].DeviceVersion = version
		}
	}
	return nil
}

func (m *Memory) CountBindings(_ context.Context, hostPub []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, b := range m.bindings {
		if bytes.Equal(b.HostPub, hostPub) && !b.Revoked {
			n++
		}
	}
	return n, nil
}

func (m *Memory) AppendTrace(_ context.Context, ev TraceEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	m.trace = append(m.trace, ev)
	const cap = 200
	if len(m.trace) > 4000 {
		m.trace = m.trace[len(m.trace)-4000:]
	}
	_ = cap
	return nil
}

func (m *Memory) Trace(_ context.Context, ref string) ([]TraceEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []TraceEvent
	for _, ev := range m.trace {
		if ev.Ref == ref {
			out = append(out, ev)
		}
	}
	return out, nil
}
