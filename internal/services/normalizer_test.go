package services

import "testing"

func TestNormalizeCoreCompanyName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"S.C. Instal Termic S.R.L.", "instal termic"},
		{"Asociația de Proprietari Bloc 10A", "bloc 10a"},
		{"  P.F.A. Ștefan Țăranu  ", "stefan taranu"},
	}

	for _, tc := range tests {
		got := NormalizeCoreCompanyName(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeCoreCompanyName(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeFiscalCodeAndPhone(t *testing.T) {
	if got := NormalizeFiscalCode("ro 12345678 "); got != "12345678" {
		t.Errorf("NormalizeFiscalCode failed: got %q", got)
	}
	if got := NormalizePhone("+40 722 111 222"); got != "0722111222" {
		t.Errorf("NormalizePhone(+40) failed: got %q", got)
	}
	if got := NormalizePhone("0040-744-555-666"); got != "0744555666" {
		t.Errorf("NormalizePhone(0040) failed: got %q", got)
	}
}
