package domain

import (
	"math"
	"strings"
	"unicode"
)

func ValidateIdentifier(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return NewError(CodeValidation, "%s 不能为空", name)
	}
	if len(value) > 128 {
		return NewError(CodeValidation, "%s 长度不能超过 128 字节", name)
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return NewError(CodeValidation, "%s 不能包含空白或控制字符", name)
		}
	}
	return nil
}

func ValidatePrincipal(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return NewError(CodeValidation, "%s 不能为空", name)
	}
	if len(value) > 128 {
		return NewError(CodeValidation, "%s 长度不能超过 128 字节", name)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return NewError(CodeValidation, "%s 不能包含控制字符", name)
		}
	}
	return nil
}

func ValidateChecksum(value string) error {
	if len(value) < 8 || len(value) > 256 {
		return NewError(CodeValidation, "content_checksum 长度必须在 8 到 256 字节之间")
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return NewError(CodeValidation, "content_checksum 不能包含空白或控制字符")
		}
	}
	return nil
}

func ValidateUnitScore(name string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return NewError(CodeValidation, "%s 必须是 0 到 1 之间的有限数值", name)
	}
	return nil
}

func ValidateMeasurement(name string, value float64, min, max float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		return NewError(CodeValidation, "%s 必须是 %.3f 到 %.3f 之间的有限数值", name, min, max)
	}
	return nil
}
