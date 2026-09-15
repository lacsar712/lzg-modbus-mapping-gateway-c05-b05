package port

import "github.com/bytecode/modbus-mapping-gateway/internal/domain"

// ModbusClient reads/writes holding registers over Modbus TCP.
type ModbusClient interface {
	ReadHoldingRegisters(endpoint string, unitID byte, timeoutMs int, address, quantity uint16) ([]uint16, error)
	WriteSingleRegister(endpoint string, unitID byte, timeoutMs int, address, value uint16) error
	WriteMultipleRegisters(endpoint string, unitID byte, timeoutMs int, address uint16, values []uint16) error
}

// MappingStore loads/saves YAML mapping configuration.
type MappingStore interface {
	Load() (domain.MappingConfig, string, error)
	Save(yamlText string) error
	Path() string
}
