package opsprotocol

import (
	"fmt"
	"strconv"
	"strings"
)

func ValidateReleaseVersion(value string) error {
	value = strings.TrimSpace(value)
	if len(value) < 6 || value[0] != 'v' {
		return fmt.Errorf("版本必须是 vX.Y.Z 格式的不可变标签")
	}
	core := value[1:]
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		if index == 0 || index == len(core)-1 {
			return fmt.Errorf("版本预发布标识无效")
		}
		suffix := core[index+1:]
		for _, char := range suffix {
			if !isVersionIdentifierChar(char) {
				return fmt.Errorf("版本预发布标识包含无效字符")
			}
		}
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return fmt.Errorf("版本必须是 vX.Y.Z 格式的不可变标签")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("版本号不能为空")
		}
		if len(part) > 1 && part[0] == '0' {
			return fmt.Errorf("版本号不能包含前导零")
		}
		if _, err := strconv.ParseUint(part, 10, 31); err != nil {
			return fmt.Errorf("版本号必须是非负整数")
		}
	}
	return nil
}

func CompareReleaseVersions(left string, right string) (int, error) {
	leftParts, leftSuffix, err := parseReleaseVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, rightSuffix, err := parseReleaseVersion(right)
	if err != nil {
		return 0, err
	}
	for index := range leftParts {
		if leftParts[index] < rightParts[index] {
			return -1, nil
		}
		if leftParts[index] > rightParts[index] {
			return 1, nil
		}
	}
	if leftSuffix == rightSuffix {
		return 0, nil
	}
	if leftSuffix == "" {
		return 1, nil
	}
	if rightSuffix == "" {
		return -1, nil
	}
	return strings.Compare(leftSuffix, rightSuffix), nil
}

func parseReleaseVersion(value string) ([3]uint64, string, error) {
	if err := ValidateReleaseVersion(value); err != nil {
		return [3]uint64{}, "", err
	}
	core := strings.TrimPrefix(strings.TrimSpace(value), "v")
	suffix := ""
	if index := strings.IndexAny(core, "-+"); index >= 0 {
		suffix = core[index+1:]
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	var result [3]uint64
	for index, part := range parts {
		parsed, err := strconv.ParseUint(part, 10, 31)
		if err != nil {
			return [3]uint64{}, "", err
		}
		result[index] = parsed
	}
	return result, suffix, nil
}

func isVersionIdentifierChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '.' || char == '-'
}
