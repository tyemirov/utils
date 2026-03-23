package crawler

import "fmt"

type capturingLogger struct {
	errors   []string
	warnings []string
}

func (logger *capturingLogger) Debug(string, ...interface{}) {}
func (logger *capturingLogger) Info(string, ...interface{})  {}
func (logger *capturingLogger) Warning(format string, args ...interface{}) {
	logger.warnings = append(logger.warnings, fmt.Sprintf(format, args...))
}
func (logger *capturingLogger) Error(format string, args ...interface{}) {
	logger.errors = append(logger.errors, fmt.Sprintf(format, args...))
}
