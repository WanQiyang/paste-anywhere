package storage

import (
	"errors"
	"strings"
)

var (
	ErrInvalidName = errors.New("用户名和设备名只能包含英文字母和数字，长度为 1–128")
	ErrNameExists  = errors.New("名称已存在")
)

func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for _, c := range name {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

type NamedDevice struct {
	Name   string `json:"name"`
	User   string `json:"user"`
	Device string `json:"device"`
}

func (s *Store) NamedDevices() ([]NamedDevice, error) {
	rows, err := s.db.Query(`SELECT u.name || '-' || d.name,u.name,d.name FROM devices d JOIN users u ON u.id=d.user_id ORDER BY u.name,d.name,d.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NamedDevice{}
	for rows.Next() {
		var d NamedDevice
		if err := rows.Scan(&d.Name, &d.User, &d.Device); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) DeleteUserByName(name string) error {
	if !ValidName(name) {
		return ErrInvalidName
	}
	r, err := s.db.Exec("DELETE FROM users WHERE name=?", name)
	return affected(r, err)
}

func (s *Store) DeleteDeviceByName(name string) error {
	user, device, ok := strings.Cut(name, "-")
	if !ok || !ValidName(user) || !ValidName(device) {
		return ErrInvalidName
	}
	r, err := s.db.Exec("DELETE FROM devices WHERE user_id=(SELECT id FROM users WHERE name=?) AND name=?", user, device)
	return affected(r, err)
}

func (s *Store) RevokeDevice(id string) error {
	r, err := s.db.Exec("DELETE FROM devices WHERE id=?", id)
	return affected(r, err)
}

func (s *Store) DeviceIdentity(id string) (string, error) {
	var name string
	err := s.db.QueryRow("SELECT u.name || '-' || d.name FROM devices d JOIN users u ON u.id=d.user_id WHERE d.id=?", id).Scan(&name)
	return name, err
}
