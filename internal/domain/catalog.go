package domain

import "strings"

// RoleType models the finite set of platform user roles.
type RoleType string

const (
	RoleVisitor       RoleType = "visitor"
	RoleLocatar       RoleType = "locatar"
	RoleAsociatie     RoleType = "asociatie"
	RolePrestator     RoleType = "prestator"
	RolePlatformAdmin RoleType = "platform_admin"
)

// SessionView holds the authenticated user context passed into Templ layouts.
type SessionView struct {
	UserID       int64
	Email        string
	Name         string
	Role         RoleType
	IsSuperAdmin bool
}

// RoleLabel returns the canonical Romanian role badge label.
func (s SessionView) RoleLabel() string {
	if s.IsSuperAdmin || s.Role == RolePlatformAdmin {
		return "Super Admin"
	}
	switch s.Role {
	case RoleLocatar:
		return "Locatar"
	case RoleAsociatie:
		return "Administrator Asociație"
	case RolePrestator:
		return "Membru Profesionist"
	default:
		return "Vizitator"
	}
}

// IsLoggedIn returns true when the session belongs to an authenticated user.
func (s SessionView) IsLoggedIn() bool {
	return s.Role != "" && s.Role != RoleVisitor && s.UserID > 0
}

// ServiceCategories contains the 14 canonical technical intervention categories.
var ServiceCategories = []string{
	"Instalații termice",
	"Instalații electrice",
	"Inspecții video și termice țevi",
	"Revizii centrale termice",
	"Servicii DDD",
	"Hidroizolații acoperișuri",
	"Interfoane și securitate",
	"Instalații sanitare",
	"Desfundări canalizare",
	"Revizii gaze naturale",
	"Montaj aer condiționat",
	"Curățenie scări de bloc",
	"Amenajări interioare",
	"Reparații acoperișuri",
}

// Counties contains all 42 Romanian counties (județe + București).
var Counties = []string{
	"Alba", "Arad", "Argeș", "Bacău", "Bihor", "Bistrița-Năsăud", "Botoșani", "Brașov",
	"Brăila", "București", "Buzău", "Caraș-Severin", "Călărași", "Cluj", "Constanța",
	"Covasna", "Dâmbovița", "Dolj", "Galați", "Giurgiu", "Gorj", "Harghita",
	"Hunedoara", "Ialomița", "Iași", "Ilfov", "Maramureș", "Mehedinți", "Mureș",
	"Neamț", "Olt", "Prahova", "Satu Mare", "Sălaj", "Sibiu", "Suceava",
	"Teleorman", "Timiș", "Tulcea", "Vaslui", "Vâlcea", "Vrancea",
}

// CanonicalSEOKeywords contains the exhaustive SEO keyword list for InstalBloc.
var CanonicalSEOKeywords = strings.Join([]string{
	"InstalBloc", "instalbloc.ro", "panou lucrari instalbloc",
	"platforma asociatii de proprietari", "aplicatie asociatii de proprietari",
	"administrare tehnica blocuri", "avizier digital asociatie proprietari",
	"cod qr avarii bloc", "fond de reparatii asociatie proprietari",
	"proces verbal receptie lucrari bloc", "instalator sanitar", "instalator autorizat",
	"instalator urgenta non stop", "avarie apa bloc", "teava sparta subsol bloc",
	"inundatie subsol bloc", "inlocuire coloane apa rece apa calda",
	"desfundare canalizare bloc", "reparatii instalatii sanitare",
	"reparatii instalatii termice", "montaj centrala termica",
	"verificare instalatii gaze ANRE", "electrician autorizat ANRE",
	"reparatii interfoane si videointerfoane bloc", "hidroizolatii terasa bloc",
	"reparatii acoperis bloc infiltratii ploaie", "curatenie asociatii de proprietari",
	"deratizare dezinsectie dezinfectie DDD bloc",
}, ", ")

var categoryDiacritics = strings.NewReplacer(
	"ă", "a", "Ă", "a",
	"â", "a", "Â", "a",
	"î", "i", "Î", "i",
	"ș", "s", "Ș", "s",
	"ş", "s", "Ş", "s",
	"ț", "t", "Ț", "t",
	"ţ", "t", "Ţ", "t",
)

var legacySlugMap = map[string]string{
	"instalasii-termice":              "instalatii-termice",
	"instalasii-electrice":            "instalatii-electrice",
	"inspecsii-video-si-termice-sevi": "inspectii-video-si-termice-tevi",
	"hidroizolasii-acoperisuri":       "hidroizolatii-acoperisuri",
	"instalasii-sanitare":             "instalatii-sanitare",
	"montaj-aer-condisionat":          "montaj-aer-conditionat",
	"curasenie-scari-de-bloc":         "curatenie-scari-de-bloc",
	"reparasii-acoperisuri":           "reparatii-acoperisuri",
}

// NormalizeCategory converts a Romanian category name into a canonical URL slug.
func NormalizeCategory(str string) string {
	ascii := categoryDiacritics.Replace(strings.TrimSpace(str))
	lower := strings.ToLower(ascii)
	var b strings.Builder
	lastDash := true
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// MatchesCategoryFilter checks if a ticket category matches a selected slug or label.
func MatchesCategoryFilter(ticketCategory, filterSlugOrName string) bool {
	if strings.TrimSpace(filterSlugOrName) == "" {
		return true
	}
	if strings.TrimSpace(ticketCategory) == "" {
		return false
	}
	normTicket := NormalizeCategory(ticketCategory)
	normFilter, ok := legacySlugMap[filterSlugOrName]
	if !ok {
		normFilter = NormalizeCategory(filterSlugOrName)
	}
	if mappedTicket, exists := legacySlugMap[normTicket]; exists && mappedTicket == normFilter {
		return true
	}
	return normTicket == normFilter || strings.EqualFold(ticketCategory, filterSlugOrName)
}

// IsValidCounty checks whether the given county name matches one of the 42 canonical counties.
func IsValidCounty(county string) bool {
	trimmed := strings.TrimSpace(county)
	if trimmed == "" {
		return false
	}
	for _, c := range Counties {
		if strings.EqualFold(c, trimmed) {
			return true
		}
	}
	return false
}

// IsValidCategory checks whether the given category matches one of the 14 canonical service categories.
func IsValidCategory(category string) bool {
	trimmed := strings.TrimSpace(category)
	if trimmed == "" {
		return false
	}
	for _, cat := range ServiceCategories {
		if strings.EqualFold(cat, trimmed) || NormalizeCategory(cat) == NormalizeCategory(trimmed) {
			return true
		}
	}
	return false
}

