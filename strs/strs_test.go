package strs

import (
	"reflect"
	"testing"
)

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "пустая строка — nil", in: "", want: nil},
		{name: "одно значение", in: "https://a.zvonilka.space", want: []string{"https://a.zvonilka.space"}},
		{name: "несколько значений", in: "https://a,https://b", want: []string{"https://a", "https://b"}},
		{name: "пробелы не тримаются", in: "a, b", want: []string{"a", " b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := SplitCSV(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("SplitCSV(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
