package config

import "fmt"

type Diagnostic struct {
	Code        string `json:"code"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	ConfigPath  string `json:"config_path"`
	OperationID string `json:"operation_id,omitempty"`
	Message     string `json:"message"`
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s: %s", d.File, d.Line, d.Column, d.Code, d.Message)
}
