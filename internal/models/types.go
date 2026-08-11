package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringSlice is a []string that serializes to/from a MySQL JSON column.
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal([]string(s))
	return string(b), err
}

func (s *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*s = StringSlice{}
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("StringSlice: cannot scan type %T", value)
	}
	return json.Unmarshal(b, s)
}
