package delivery

// RunPreflight evaluates only the authoritative platform configuration
// runtime. Callers reject historical versions before invoking it.
func RunPreflight(version DeliveryPlanVersion) []PreflightCheck {
	if !version.IsPlatformConfigurationV2() {
		return nil
	}
	return runPlatformConfigurationPreflight(version)
}

func check(code string, severity CheckSeverity, passed bool, successMessage, failureMessage string, repair RepairTarget) PreflightCheck {
	if passed {
		return PreflightCheck{Code: code, Severity: severity, Passed: true, Message: successMessage}
	}
	return PreflightCheck{Code: code, Severity: severity, Passed: false, Message: failureMessage, Repair: &repair}
}
