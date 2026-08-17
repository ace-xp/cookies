package provider

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shikanon/cookies/internal/platform/servicecatalog"
)

var (
	// ErrServiceConfigurationConflict means someone else saved a newer
	// revision while this form was open.
	ErrServiceConfigurationConflict = errors.New("service configuration version conflict")
	// ErrServiceProbeFailed means the probe rejected the input, so nothing was
	// written. Configuration that cannot be reached is not persisted — that is
	// what keeps a typo from taking a capability down until someone notices.
	ErrServiceProbeFailed = errors.New("service configuration probe failed")
)

// ServiceConfigurationInputError carries a message that is safe to show the
// operator verbatim.
type ServiceConfigurationInputError struct{ Message string }

func (e ServiceConfigurationInputError) Error() string { return e.Message }

// ServiceConfiguration is the read model for one catalog entry. Secrets appear
// only in MaskedSecrets, never in Values.
type ServiceConfiguration struct {
	Code               string
	Configured         bool
	Values             map[string]string
	MaskedSecrets      map[string]string
	CredentialReadable bool
	Version            int64
	UpdatedAt          time.Time
	LastProbe          servicecatalog.Result
	LastProbedAt       *time.Time
	// EnvironmentFallback reports what the deployment environment would supply
	// when nothing is stored, so the page can say "current value comes from
	// deployment configuration" instead of showing an empty form.
	EnvironmentFallback map[string]string
}

// ServiceConfigurationInput is what the settings page submits. A field absent
// from Values is left untouched; this is how an omitted secret keeps the
// stored credential.
type ServiceConfigurationInput struct {
	Code            string
	Values          map[string]string
	ExpectedVersion *int64
}

// normalizeServiceConfigurationInput validates a submission against the
// catalog's declaration and returns a value map containing only declared
// fields. Anything the catalog does not declare is dropped rather than stored.
func normalizeServiceConfigurationInput(input ServiceConfigurationInput, allowInsecureHTTP bool) (ServiceConfigurationInput, error) {
	service, found := servicecatalog.Find(input.Code)
	if !found {
		return ServiceConfigurationInput{}, ServiceConfigurationInputError{
			Message: fmt.Sprintf("未知的服务编码 %q", input.Code),
		}
	}
	if service.Tier != servicecatalog.TierEditable {
		return ServiceConfigurationInput{}, ServiceConfigurationInputError{
			Message: service.DisplayName + "不支持在页面上修改",
		}
	}

	normalized := ServiceConfigurationInput{
		Code:            input.Code,
		ExpectedVersion: input.ExpectedVersion,
		Values:          map[string]string{},
	}
	for _, field := range service.Fields {
		value := strings.TrimSpace(input.Values[field.Name])

		// A secret that arrived blank or absent stays out of the map, so the
		// caller keeps the stored credential. Erasing on blank would break
		// every save that only meant to change the address.
		if field.Kind == servicecatalog.FieldSecret && value == "" {
			continue
		}
		if field.Required && value == "" {
			return ServiceConfigurationInput{}, ServiceConfigurationInputError{
				Message: field.Label + "不能为空",
			}
		}
		if field.IsAddress() {
			address, err := normalizeAddress(value, allowInsecureHTTP)
			if err != nil {
				return ServiceConfigurationInput{}, ServiceConfigurationInputError{
					Message: field.Label + err.Message,
				}
			}
			value = address
		}
		normalized.Values[field.Name] = value
	}
	return normalized, nil
}

// normalizeAddress returns the canonical form of an upstream address. The
// returned error message is a suffix, so the caller can prefix the field's own
// label.
func normalizeAddress(value string, allowInsecureHTTP bool) (string, *ServiceConfigurationInputError) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", &ServiceConfigurationInputError{Message: "不是一个有效的地址"}
	}
	if parsed.Scheme != "https" && !(allowInsecureHTTP && parsed.Scheme == "http") {
		return "", &ServiceConfigurationInputError{Message: "必须是 https 地址"}
	}
	// A pasted address often carries a trailing slash. Keeping both forms would
	// produce two stored values that look identical on the page.
	return strings.TrimRight(parsed.String(), "/"), nil
}
