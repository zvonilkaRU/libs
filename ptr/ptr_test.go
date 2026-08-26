package ptr

import "testing"

func TestValOrNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantNil bool
	}{
		{name: "пустая строка — nil", in: "", wantNil: true},
		{name: "непустая строка — указатель", in: "icon.png", want: "icon.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ValOrNil(tt.in)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("ValOrNil(%q) = %v, want nil", tt.in, *got)
				}

				return
			}
			if got == nil || *got != tt.want {
				t.Fatalf("ValOrNil(%q) = %v, want указатель на %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValOrNilZeroOtherTypes(t *testing.T) {
	t.Parallel()

	// zero не-строковых типов тоже уходит в nil.
	if got := ValOrNil(0); got != nil {
		t.Fatalf("ValOrNil(0) = %v, want nil", *got)
	}

	if got := ValOrNil(false); got != nil {
		t.Fatalf("ValOrNil(false) = %v, want nil", *got)
	}

	if got := ValOrNil(7); got == nil || *got != 7 {
		t.Fatalf("ValOrNil(7) = %v, want указатель на 7", got)
	}
}

func TestVal(t *testing.T) {
	t.Parallel()

	var nilStr *string
	if got := Val(nilStr); got != "" {
		t.Fatalf("Val(nil) = %q, want пустую строку", got)
	}

	var nilInt *int
	if got := Val(nilInt); got != 0 {
		t.Fatalf("Val(nil) = %d, want 0", got)
	}

	s := "x"
	if got := Val(&s); got != "x" {
		t.Fatalf("Val(&s) = %q, want %q", got, "x")
	}
}
