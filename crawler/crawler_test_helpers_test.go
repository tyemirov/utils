package crawler

import "fmt"

type savedFile struct {
	productID string
	fileName  string
	content   []byte
}

type stubFilePersister struct {
	saved   []string
	records []savedFile
	err     error
}

func (stub *stubFilePersister) Save(productID, fileName string, content []byte) error {
	stub.saved = append(stub.saved, productID+":"+fileName)
	stub.records = append(stub.records, savedFile{
		productID: productID,
		fileName:  fileName,
		content:   append([]byte(nil), content...),
	})
	return stub.err
}

func (stub *stubFilePersister) Close() error {
	return nil
}

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
