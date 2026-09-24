package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SQLite is the durable Store. It keeps the same records as Memory: token
// hashes, not raw tokens, and no session keys.
type SQLite struct {
	db *gorm.DB
}

type hostRow struct {
	ID        uint `gorm:"primaryKey"`
	Pub       []byte
	TokenHash []byte `gorm:"index"`
	Name      string
	Version   string
	Created   time.Time
}

func (hostRow) TableName() string { return "hosts" }

func (r hostRow) host() Host {
	return Host{Pub: clone(r.Pub), TokenHash: clone(r.TokenHash), Name: r.Name, Version: r.Version, Created: r.Created}
}

type pairingRow struct {
	ID        string `gorm:"primaryKey"`
	HostPub   []byte
	CodeHash  []byte
	Expires   time.Time
	Consumed  bool
	SessionID []byte
}

func (pairingRow) TableName() string { return "pairings" }

func (r pairingRow) pairing() Pairing {
	return Pairing{
		ID: r.ID, HostPub: clone(r.HostPub), CodeHash: clone(r.CodeHash),
		Expires: r.Expires, Consumed: r.Consumed, SessionID: clone(r.SessionID),
	}
}

type bindingRow struct {
	ID            string `gorm:"primaryKey"`
	HostPub       []byte
	DevicePub     []byte
	TicketHash    []byte
	DeviceName    string
	DeviceModel   string
	DeviceVersion string
	Revoked       bool
	Created       time.Time
	LastConnected time.Time
	SessionID     []byte
}

func (bindingRow) TableName() string { return "bindings" }

func (r bindingRow) binding() Binding {
	return Binding{
		ID: r.ID, HostPub: clone(r.HostPub), DevicePub: clone(r.DevicePub),
		TicketHash: clone(r.TicketHash), DeviceName: r.DeviceName, DeviceModel: r.DeviceModel, DeviceVersion: r.DeviceVersion,
		Revoked: r.Revoked, Created: r.Created, LastConnected: r.LastConnected, SessionID: clone(r.SessionID),
	}
}

type traceRow struct {
	ID     uint   `gorm:"primaryKey"`
	Ref    string `gorm:"index"`
	At     time.Time
	Kind   string
	PeerFP string
	Bytes  int
	Path   string
	Note   string
}

func (traceRow) TableName() string { return "traces" }

func (r traceRow) event() TraceEvent {
	return TraceEvent{Ref: r.Ref, At: r.At, Kind: r.Kind, PeerFP: r.PeerFP, Bytes: r.Bytes, Path: r.Path, Note: r.Note}
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}

// OpenSQLite opens or creates a database file and migrates the control tables.
func OpenSQLite(path string) (*SQLite, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("store: empty sqlite path")
	}
	dsn := path
	if !strings.Contains(path, "?") {
		dsn += "?_pragma=busy_timeout(5000)"
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("store: sqlite: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&hostRow{}, &pairingRow{}, &bindingRow{}, &traceRow{}); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return &SQLite{db: db}, nil
}

// Close releases the database file.
func (s *SQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (s *SQLite) PutHost(ctx context.Context, h Host) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := loadHosts(tx)
		if err != nil {
			return err
		}
		hosts := make([]Host, len(rows))
		for i := range rows {
			hosts[i] = rows[i].host()
		}
		if i := hostIndex(hosts, h); i >= 0 {
			merged := mergeHost(rows[i].host(), h)
			row := hostRow{
				ID: rows[i].ID, Pub: clone(merged.Pub), TokenHash: clone(merged.TokenHash),
				Name: merged.Name, Version: merged.Version, Created: merged.Created,
			}
			return tx.Save(&row).Error
		}
		row := hostRow{Pub: clone(h.Pub), TokenHash: clone(h.TokenHash), Name: h.Name, Version: h.Version, Created: h.Created}
		if row.Created.IsZero() {
			row.Created = time.Now().UTC()
		}
		return tx.Create(&row).Error
	})
}

func (s *SQLite) HostByTokenHash(ctx context.Context, hash []byte) (Host, error) {
	rows, err := loadHosts(s.db.WithContext(ctx))
	if err != nil {
		return Host{}, err
	}
	for _, row := range rows {
		if bytes.Equal(row.TokenHash, hash) {
			return row.host(), nil
		}
	}
	return Host{}, ErrNotFound
}

func (s *SQLite) HostByPub(ctx context.Context, pub []byte) (Host, error) {
	rows, err := loadHosts(s.db.WithContext(ctx))
	if err != nil {
		return Host{}, err
	}
	for _, row := range rows {
		if bytes.Equal(row.Pub, pub) {
			return row.host(), nil
		}
	}
	return Host{}, ErrNotFound
}

func (s *SQLite) ListHosts(ctx context.Context) ([]Host, error) {
	rows, err := loadHosts(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.host())
	}
	return out, nil
}

func loadHosts(tx *gorm.DB) ([]hostRow, error) {
	var rows []hostRow
	err := tx.Order("id asc").Find(&rows).Error
	return rows, err
}

func (s *SQLite) PutPairing(ctx context.Context, p Pairing) error {
	row := pairingRow{
		ID: p.ID, HostPub: clone(p.HostPub), CodeHash: clone(p.CodeHash),
		Expires: p.Expires, Consumed: p.Consumed, SessionID: clone(p.SessionID),
	}
	return s.db.WithContext(ctx).Create(&row).Error
}

func (s *SQLite) ConsumePairing(ctx context.Context, codeHash []byte) (Pairing, error) {
	var out Pairing
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []pairingRow
		if err := tx.Find(&rows).Error; err != nil {
			return err
		}
		now := time.Now()
		for i := range rows {
			if !bytes.Equal(rows[i].CodeHash, codeHash) {
				continue
			}
			if rows[i].Consumed {
				return ErrConsumed
			}
			if now.After(rows[i].Expires) {
				return ErrExpired
			}
			rows[i].Consumed = true
			if err := tx.Save(&rows[i]).Error; err != nil {
				return err
			}
			out = rows[i].pairing()
			out.Consumed = true
			return nil
		}
		return ErrNotFound
	})
	return out, err
}

func (s *SQLite) PairingByID(ctx context.Context, id string) (Pairing, error) {
	var row pairingRow
	err := s.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Pairing{}, ErrNotFound
	}
	if err != nil {
		return Pairing{}, err
	}
	return row.pairing(), nil
}

func (s *SQLite) PutBinding(ctx context.Context, b Binding) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := loadBindings(tx)
		if err != nil {
			return err
		}
		n := 0
		for _, row := range rows {
			if bytes.Equal(row.HostPub, b.HostPub) && !row.Revoked {
				n++
			}
		}
		if n >= MaxBindingsPerHost {
			return ErrLimit
		}
		row := bindingRow{
			ID: b.ID, HostPub: clone(b.HostPub), DevicePub: clone(b.DevicePub),
			TicketHash: clone(b.TicketHash), DeviceName: b.DeviceName, DeviceModel: b.DeviceModel, DeviceVersion: b.DeviceVersion,
			Revoked: b.Revoked, Created: b.Created, LastConnected: b.LastConnected, SessionID: clone(b.SessionID),
		}
		if row.Created.IsZero() {
			row.Created = time.Now().UTC()
		}
		return tx.Create(&row).Error
	})
}

func (s *SQLite) BindingByTicketHash(ctx context.Context, hash []byte) (Binding, error) {
	rows, err := loadBindings(s.db.WithContext(ctx))
	if err != nil {
		return Binding{}, err
	}
	for _, row := range rows {
		if bytes.Equal(row.TicketHash, hash) {
			if row.Revoked {
				return Binding{}, ErrRevoked
			}
			return row.binding(), nil
		}
	}
	return Binding{}, ErrNotFound
}

func (s *SQLite) BindingByPeers(ctx context.Context, hostPub, devicePub []byte) (Binding, error) {
	rows, err := loadBindings(s.db.WithContext(ctx))
	if err != nil {
		return Binding{}, err
	}
	for _, row := range rows {
		if bytes.Equal(row.HostPub, hostPub) && bytes.Equal(row.DevicePub, devicePub) {
			if row.Revoked {
				return Binding{}, ErrRevoked
			}
			return row.binding(), nil
		}
	}
	return Binding{}, ErrNotFound
}

func (s *SQLite) ListBindings(ctx context.Context, hostPub []byte) ([]Binding, error) {
	rows, err := loadBindings(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	var out []Binding
	for _, row := range rows {
		if bytes.Equal(row.HostPub, hostPub) && !row.Revoked {
			out = append(out, row.binding())
		}
	}
	return out, nil
}

func (s *SQLite) ListAllBindings(ctx context.Context) ([]Binding, error) {
	rows, err := loadBindings(s.db.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]Binding, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.binding())
	}
	return out, nil
}

func loadBindings(tx *gorm.DB) ([]bindingRow, error) {
	var rows []bindingRow
	err := tx.Order("created asc").Find(&rows).Error
	return rows, err
}

func (s *SQLite) RevokeBinding(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row bindingRow
		err := tx.First(&row, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		row.Revoked = true
		return tx.Save(&row).Error
	})
}

func (s *SQLite) NoteDeviceSeen(ctx context.Context, devicePub []byte, at time.Time) error {
	if len(devicePub) != 32 || at.IsZero() {
		return nil
	}
	at = at.UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := loadBindings(tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !bytes.Equal(row.DevicePub, devicePub) {
				continue
			}
			if !row.LastConnected.IsZero() && !at.After(row.LastConnected) {
				continue
			}
			row.LastConnected = at
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLite) SetDeviceLabels(ctx context.Context, devicePub []byte, name, model, version string) error {
	if len(devicePub) != 32 || (name == "" && model == "" && version == "") {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := loadBindings(tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if !bytes.Equal(row.DevicePub, devicePub) {
				continue
			}
			if name != "" {
				row.DeviceName = name
			}
			if model != "" {
				row.DeviceModel = model
			}
			if version != "" {
				row.DeviceVersion = version
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *SQLite) CountBindings(ctx context.Context, hostPub []byte) (int, error) {
	list, err := s.ListBindings(ctx, hostPub)
	if err != nil {
		return 0, err
	}
	return len(list), nil
}

// traceRetain is how many trace rows SQLite keeps. Tests lower it.
var traceRetain = 4000

func (s *SQLite) AppendTrace(ctx context.Context, ev TraceEvent) error {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := traceRow{Ref: ev.Ref, At: ev.At, Kind: ev.Kind, PeerFP: ev.PeerFP, Bytes: ev.Bytes, Path: ev.Path, Note: ev.Note}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&traceRow{}).Count(&n).Error; err != nil {
			return err
		}
		if n <= int64(traceRetain) {
			return nil
		}
		extra := int(n - int64(traceRetain))
		return tx.Exec("DELETE FROM traces WHERE id IN (SELECT id FROM traces ORDER BY id ASC LIMIT ?)", extra).Error
	})
}

func (s *SQLite) Trace(ctx context.Context, ref string) ([]TraceEvent, error) {
	var rows []traceRow
	if err := s.db.WithContext(ctx).Where("ref = ?", ref).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TraceEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.event())
	}
	return out, nil
}
