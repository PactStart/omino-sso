package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringList 是 JSON 数组列（roles / scopes / amr 等）的通用类型。
type StringList []string

func (s StringList) Value() (driver.Value, error) {
	if s == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]string(s))
}

func (s *StringList) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	var raw []byte
	switch v := src.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("StringList: unsupported scan type %T", src)
	}
	if len(raw) == 0 {
		*s = nil
		return nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	*s = out
	return nil
}
