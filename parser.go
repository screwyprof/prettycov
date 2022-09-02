package prettycov

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

func ParseHTML(r io.Reader) (Coverage, error) {
	var (
		isOption bool
		items    []CoverageItem
	)

	z := html.NewTokenizer(r)

loop:
	for {
		tt := z.Next()

		switch {
		case tt == html.ErrorToken:
			break loop // End of the document, we're done
		case tt == html.StartTagToken:
			t := z.Token()

			if t.Data == "option" {
				isOption = true
			}
		case tt == html.TextToken:
			t := z.Token()

			if !isOption {
				continue
			}

			item, err := parseOption(t.Data)
			if err != nil {
				return Coverage{}, err
			}

			items = append(items, item)

			isOption = false
		}
	}

	return Coverage{
		Items: items,
	}, nil
}

func parseOption(data string) (CoverageItem, error) {
	parts := strings.Fields(data)
	s := strings.Trim(parts[1], "()")

	percent, err := parsePercentage(s)
	if err != nil {
		return CoverageItem{}, err
	}

	return CoverageItem{
		File:     parts[0],
		Coverage: percent,
	}, nil
}

func ParseText(coverage io.Reader) (Coverage, error) {
	var (
		items []CoverageItem
		total float64
	)

	scanner := bufio.NewScanner(coverage)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "total") {
			t, err := parsePercentage(strings.Fields(line)[2])
			if err != nil {
				return Coverage{}, err
			}

			total = t

			break
		}

		item, err := parseItem(line)
		if err != nil {
			return Coverage{}, err
		}

		items = append(items, item)
	}

	return Coverage{
		Items: items,
		Total: total,
	}, nil
}

func parseItem(line string) (CoverageItem, error) {
	cols := strings.Fields(line)

	if len(cols) < 3 {
		return CoverageItem{}, ErrInvalidLineFormat
	}

	cov, err := parsePercentage(cols[2])
	if err != nil {
		return CoverageItem{}, fmt.Errorf("%w: %v", ErrInvalidLineFormat, err)
	}

	return CoverageItem{
		File:     trimAfter(cols[0], ":"),
		Coverage: cov,
	}, nil
}

func trimAfter(s, cutset string) string {
	if idx := strings.Index(s, cutset); idx != -1 {
		return s[:idx]
	}

	return s
}

func parsePercentage(line string) (float64, error) {
	total, err := strconv.ParseFloat(strings.TrimRight(line, "%"), 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse coverage percentage: %w ", err)
	}

	return total, nil
}
