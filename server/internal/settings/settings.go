// Package settings owns the panel's global rendering preferences —
// the values that shape generated subscription content (the render
// phase consumes them; nothing here touches the core). Storage is the
// store's key/value settings table; this package owns the keys, the
// defaults and the validation.
package settings

import (
	"context"
	"strconv"
	"strings"
	"time"

	"singbox-ui/server/internal/store"
)

// Error mirrors the other modules' typed API failure.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

func errValidation(msg string) *Error {
	return &Error{Status: 400, Code: "validation", Message: msg}
}

type Service struct {
	st *store.Store
}

func NewService(st *store.Store) *Service { return &Service{st: st} }

// Setting keys in the store's settings table.
const (
	keyWebRTCMode      = "webrtc_mode"       // proxy | reject | off
	keyDNSFallback     = "dns_fallback"      // foreign | domestic
	keyDNSRespectRules = "dns_respect_rules" // true | false

	keyNodeName          = "sub_node_name"           // node name prefix, "" = none
	keyLinkFormat        = "sub_link_format"         // clash | singbox
	keySkipCertVerify    = "sub_skip_cert_verify"    // true | false
	keyPublicHost        = "sub_public_host"         // "" = derive automatically
	keyShowUserinfo      = "sub_show_userinfo"       // true | false
	keyUpdateIntervalHrs = "sub_update_interval_hrs" // 0 = unspecified

	keyTextSelectable = "text_selectable" // true | false, default false

	keyTimezone = "timezone" // IANA name, default Asia/Shanghai

	keyShowWarnings = "show_warnings" // true | false, default true
)

// View is the full settings state exchanged with the frontend.
type View struct {
	WebRTCMode      string `json:"webrtcMode"`
	DNSFallback     string `json:"dnsFallback"`
	DNSRespectRules bool   `json:"dnsRespectRules"`

	NodeName          string `json:"nodeName"`
	LinkFormat        string `json:"linkFormat"`
	SkipCertVerify    bool   `json:"skipCertVerify"`
	PublicHost        string `json:"publicHost"`
	ShowUserinfo      bool   `json:"showUserinfo"`
	UpdateIntervalHrs int    `json:"updateIntervalHours"`

	TextSelectable bool   `json:"textSelectable"`
	Timezone       string `json:"timezone"`
	ShowWarnings   bool   `json:"showWarnings"`
}

// Update accepts partial changes; nil fields stay untouched.
type Update struct {
	WebRTCMode      *string `json:"webrtcMode"`
	DNSFallback     *string `json:"dnsFallback"`
	DNSRespectRules *bool   `json:"dnsRespectRules"`

	NodeName          *string `json:"nodeName"`
	LinkFormat        *string `json:"linkFormat"`
	SkipCertVerify    *bool   `json:"skipCertVerify"`
	PublicHost        *string `json:"publicHost"`
	ShowUserinfo      *bool   `json:"showUserinfo"`
	UpdateIntervalHrs *int    `json:"updateIntervalHours"`

	TextSelectable *bool   `json:"textSelectable"`
	Timezone       *string `json:"timezone"`
	ShowWarnings   *bool   `json:"showWarnings"`
}

func (s *Service) get(ctx context.Context, key, def string) (string, error) {
	v, ok, err := s.st.GetSetting(ctx, key)
	if err != nil {
		return "", err
	}
	if !ok {
		return def, nil
	}
	return v, nil
}

func (s *Service) getBool(ctx context.Context, key string, def bool) (bool, error) {
	v, err := s.get(ctx, key, strconv.FormatBool(def))
	if err != nil {
		return false, err
	}
	return v == "true", nil
}

func (s *Service) Get(ctx context.Context) (View, error) {
	mode, err := s.get(ctx, keyWebRTCMode, "proxy")
	if err != nil {
		return View{}, err
	}
	fallback, err := s.get(ctx, keyDNSFallback, "foreign")
	if err != nil {
		return View{}, err
	}
	respect, err := s.getBool(ctx, keyDNSRespectRules, true)
	if err != nil {
		return View{}, err
	}
	nodeName, err := s.get(ctx, keyNodeName, "")
	if err != nil {
		return View{}, err
	}
	linkFormat, err := s.get(ctx, keyLinkFormat, "clash")
	if err != nil {
		return View{}, err
	}
	skipCert, err := s.getBool(ctx, keySkipCertVerify, false)
	if err != nil {
		return View{}, err
	}
	publicHost, err := s.get(ctx, keyPublicHost, "")
	if err != nil {
		return View{}, err
	}
	showUserinfo, err := s.getBool(ctx, keyShowUserinfo, true)
	if err != nil {
		return View{}, err
	}
	intervalStr, err := s.get(ctx, keyUpdateIntervalHrs, "24")
	if err != nil {
		return View{}, err
	}
	interval, _ := strconv.Atoi(intervalStr)
	textSelectable, err := s.getBool(ctx, keyTextSelectable, false)
	if err != nil {
		return View{}, err
	}
	timezone, err := s.get(ctx, keyTimezone, "Asia/Shanghai")
	if err != nil {
		return View{}, err
	}
	showWarnings, err := s.getBool(ctx, keyShowWarnings, true)
	if err != nil {
		return View{}, err
	}
	return View{
		WebRTCMode:        mode,
		DNSFallback:       fallback,
		DNSRespectRules:   respect,
		NodeName:          nodeName,
		LinkFormat:        linkFormat,
		SkipCertVerify:    skipCert,
		PublicHost:        publicHost,
		ShowUserinfo:      showUserinfo,
		UpdateIntervalHrs: interval,
		TextSelectable:    textSelectable,
		Timezone:          timezone,
		ShowWarnings:      showWarnings,
	}, nil
}

func (s *Service) Update(ctx context.Context, u Update) (View, error) {
	if u.WebRTCMode != nil {
		switch *u.WebRTCMode {
		case "proxy", "reject", "off":
		default:
			return View{}, errValidation("webrtcMode must be proxy, reject or off")
		}
		if err := s.st.SetSetting(ctx, keyWebRTCMode, *u.WebRTCMode); err != nil {
			return View{}, err
		}
	}
	if u.DNSFallback != nil {
		switch *u.DNSFallback {
		case "foreign", "domestic":
		default:
			return View{}, errValidation("dnsFallback must be foreign or domestic")
		}
		if err := s.st.SetSetting(ctx, keyDNSFallback, *u.DNSFallback); err != nil {
			return View{}, err
		}
	}
	if u.DNSRespectRules != nil {
		if err := s.st.SetSetting(ctx, keyDNSRespectRules, strconv.FormatBool(*u.DNSRespectRules)); err != nil {
			return View{}, err
		}
	}
	if u.NodeName != nil {
		name := strings.TrimSpace(*u.NodeName)
		if len([]rune(name)) > 64 || strings.ContainsAny(name, "\r\n") {
			return View{}, errValidation("nodeName must be at most 64 characters on one line")
		}
		if err := s.st.SetSetting(ctx, keyNodeName, name); err != nil {
			return View{}, err
		}
	}
	if u.LinkFormat != nil {
		switch *u.LinkFormat {
		case "clash", "singbox":
		default:
			return View{}, errValidation("linkFormat must be clash or singbox")
		}
		if err := s.st.SetSetting(ctx, keyLinkFormat, *u.LinkFormat); err != nil {
			return View{}, err
		}
	}
	if u.SkipCertVerify != nil {
		if err := s.st.SetSetting(ctx, keySkipCertVerify, strconv.FormatBool(*u.SkipCertVerify)); err != nil {
			return View{}, err
		}
	}
	if u.PublicHost != nil {
		host := strings.TrimSpace(*u.PublicHost)
		if host != "" && (strings.ContainsAny(host, " \t\r\n/") || strings.Contains(host, "://")) {
			return View{}, errValidation("publicHost must be a bare domain or IP (no scheme, no path)")
		}
		if len(host) > 255 {
			return View{}, errValidation("publicHost is too long")
		}
		if err := s.st.SetSetting(ctx, keyPublicHost, host); err != nil {
			return View{}, err
		}
	}
	if u.ShowUserinfo != nil {
		if err := s.st.SetSetting(ctx, keyShowUserinfo, strconv.FormatBool(*u.ShowUserinfo)); err != nil {
			return View{}, err
		}
	}
	if u.TextSelectable != nil {
		if err := s.st.SetSetting(ctx, keyTextSelectable, strconv.FormatBool(*u.TextSelectable)); err != nil {
			return View{}, err
		}
	}
	if u.UpdateIntervalHrs != nil {
		if *u.UpdateIntervalHrs < 0 || *u.UpdateIntervalHrs > 720 {
			return View{}, errValidation("updateIntervalHours must be between 0 and 720")
		}
		if err := s.st.SetSetting(ctx, keyUpdateIntervalHrs, strconv.Itoa(*u.UpdateIntervalHrs)); err != nil {
			return View{}, err
		}
	}
	if u.ShowWarnings != nil {
		if err := s.st.SetSetting(ctx, keyShowWarnings, strconv.FormatBool(*u.ShowWarnings)); err != nil {
			return View{}, err
		}
	}
	if u.Timezone != nil {
		zone := strings.TrimSpace(*u.Timezone)
		// LoadLocation("") silently means UTC, so reject empty outright.
		if zone == "" {
			return View{}, errValidation("timezone must be a valid IANA timezone name")
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return View{}, errValidation("timezone must be a valid IANA timezone name")
		}
		if err := s.st.SetSetting(ctx, keyTimezone, zone); err != nil {
			return View{}, err
		}
	}
	return s.Get(ctx)
}
