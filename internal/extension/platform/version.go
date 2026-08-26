// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import (
	"fmt"
	"strconv"
	"strings"
)

type semanticVersion struct {
	major, minor, patch int
	prerelease          []string
}

// checkCLIVersion accepts exact versions and whitespace/comma-separated
// comparator sets such as ">=1.2.0 <2.0.0" or ">= 1.2.0, < 2.0.0".
// Build metadata is ignored.
func checkCLIVersion(constraint, current string) error {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil
	}
	actual, err := parseSemanticVersion(current)
	if err != nil {
		if strings.TrimSpace(current) == "dev" {
			return fmt.Errorf("cannot evaluate CLI version %q; an enterprise entry point using RequiredCLIVersion must call cmd.ExecuteWithVersion with a semantic version: %w", current, err)
		}
		return fmt.Errorf("cannot evaluate CLI version %q: %w", current, err)
	}
	parts, err := splitConstraintParts(constraint)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("empty CLI version constraint")
	}
	for _, part := range parts {
		operator, expectedText := splitComparator(part)
		expected, err := parseSemanticVersion(expectedText)
		if err != nil {
			return fmt.Errorf("invalid CLI version constraint %q: %w", constraint, err)
		}
		comparison := compareSemanticVersion(actual, expected)
		matches := false
		switch operator {
		case "=", "==":
			matches = comparison == 0
		case ">":
			matches = comparison > 0
		case ">=":
			matches = comparison >= 0
		case "<":
			matches = comparison < 0
		case "<=":
			matches = comparison <= 0
		default:
			return fmt.Errorf("unsupported CLI version operator %q", operator)
		}
		if !matches {
			return fmt.Errorf("CLI %s does not satisfy %s", current, constraint)
		}
	}
	return nil
}

func splitConstraintParts(constraint string) ([]string, error) {
	fields := strings.Fields(strings.ReplaceAll(constraint, ",", " "))
	parts := make([]string, 0, len(fields))
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		if !isComparator(field) {
			parts = append(parts, field)
			continue
		}
		if index+1 >= len(fields) || isComparator(fields[index+1]) {
			return nil, fmt.Errorf("invalid CLI version constraint %q: comparator %q has no version", constraint, field)
		}
		parts = append(parts, field+fields[index+1])
		index++
	}
	return parts, nil
}

func isComparator(value string) bool {
	switch value {
	case ">=", "<=", "==", ">", "<", "=":
		return true
	default:
		return false
	}
}

func splitComparator(value string) (string, string) {
	for _, operator := range []string{">=", "<=", "==", ">", "<", "="} {
		if strings.HasPrefix(value, operator) {
			return operator, strings.TrimSpace(strings.TrimPrefix(value, operator))
		}
	}
	return "=", value
}

func parseSemanticVersion(value string) (semanticVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if buildIndex := strings.IndexByte(value, '+'); buildIndex >= 0 {
		value = value[:buildIndex]
	}
	var prerelease []string
	if preIndex := strings.IndexByte(value, '-'); preIndex >= 0 {
		prerelease = strings.Split(value[preIndex+1:], ".")
		value = value[:preIndex]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return semanticVersion{}, fmt.Errorf("version must have major.minor.patch")
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semanticVersion{}, fmt.Errorf("invalid numeric component %q", part)
		}
		numbers[index] = number
	}
	return semanticVersion{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}, nil
}

func compareSemanticVersion(left, right semanticVersion) int {
	for _, pair := range [][2]int{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0
	}
	if len(left.prerelease) == 0 {
		return 1
	}
	if len(right.prerelease) == 0 {
		return -1
	}
	for index := 0; index < len(left.prerelease) && index < len(right.prerelease); index++ {
		comparison := comparePrereleaseIdentifier(left.prerelease[index], right.prerelease[index])
		if comparison != 0 {
			return comparison
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}

func comparePrereleaseIdentifier(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
