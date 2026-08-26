// Package strs — строковые хелперы конфигурации и маппинга.
package strs

import "strings"

// SplitCSV разбирает список значений из одной строки: пустая строка →
// nil (семантика «не задано» — например, CORS «разрешить все»), иначе
// split по запятой. Пробелы вокруг значений не тримаются.
func SplitCSV(raw string) []string {
	if raw == "" {
		return nil
	}

	return strings.Split(raw, ",")
}
