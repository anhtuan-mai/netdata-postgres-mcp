// SPDX-License-Identifier: GPL-3.0-or-later

package config

// DerivedContext defines a user-specified computed metric.
// Example: memory_pressure = system.ram.used / system.ram.total * 100
type DerivedContext struct {
	Name       string `yaml:"name"`       // Output context name, e.g. "custom.memory_pressure"
	Expression string `yaml:"expression"` // Formula: "A / B * 100"
	InputA     string `yaml:"input_a"`    // Context.Dimension, e.g. "system.ram.used"
	InputB     string `yaml:"input_b"`    // Context.Dimension, e.g. "system.ram.total" (optional)
	Unit       string `yaml:"unit"`       // Output unit, e.g. "percentage"
}
