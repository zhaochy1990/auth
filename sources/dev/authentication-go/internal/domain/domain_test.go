package domain

import "testing"

func TestParsePhoneNumber(t *testing.T) {
	valid := []string{
		"13812345678",
		"19912345678",
		"16612345678",
		"15900001111",
		" 13812345678 ", // surrounding whitespace is trimmed
	}
	for _, in := range valid {
		p, err := ParsePhoneNumber(in)
		if err != nil {
			t.Fatalf("ParsePhoneNumber(%q) = error %v, want valid", in, err)
		}
		if string(p) != "13812345678" && string(p) != "19912345678" && string(p) != "16612345678" && string(p) != "15900001111" {
			t.Fatalf("ParsePhoneNumber(%q) = %q, want the bare 11 digits", in, string(p))
		}
	}
}

func TestParsePhoneNumberRejectsInvalid(t *testing.T) {
	invalid := []string{
		"",               // empty
		"1381234567",     // 10 digits
		"138123456789",   // 12 digits
		"23812345678",    // starts with 2 (not 1[3-9])
		"12812345678",    // starts with 12
		"1381234567a",    // non-digit
		"+8613812345678", // prefixed
		"138 1234 5678",  // spaced
	}
	for _, in := range invalid {
		if _, err := ParsePhoneNumber(in); err == nil {
			t.Fatalf("ParsePhoneNumber(%q) = nil error, want rejection", in)
		}
	}
}

func TestPhoneNumberMasked(t *testing.T) {
	p := PhoneNumber("13812345678")
	if got := p.Masked(); got != "138****5678" {
		t.Fatalf("Masked() = %q, want 138****5678", got)
	}
}
