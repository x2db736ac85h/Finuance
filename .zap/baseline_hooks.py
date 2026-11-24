# ZAP hook for the baseline scan (tools/zap-scan.sh). The rules file can only turn a whole rule
# down, and rule 10055 holds every finding about the content security policy (a wildcard source,
# unsafe-inline, a missing directive, ...). So this hook drops one alert of it alone: "CSP:
# Notices" (alert 10055-3) when the only thing it notices is that report-uri is deprecated.
#
# The policy names both report-uri and report-to on purpose (PLAN 24.8, the maintainer's decision of
# 2026-11-24): browsers that know report-to ignore report-uri, and the others report only through
# it. Any other notice, or the same notice next to another one, still fails the scan.

NOTICES = "10055-3"
REPORT_URI = "The report-uri directive has been deprecated in favor of the new report-to directive"
HEADINGS = {"Errors:", "Warnings:", "Infos:"}


def only_report_uri(alert):
    """True for a "CSP: Notices" alert whose one notice is the report-uri deprecation."""
    if alert.get("alertRef") != NOTICES:
        return False
    lines = [line.strip() for line in (alert.get("other") or "").splitlines()]
    notices = [line for line in lines if line and line not in HEADINGS]
    return notices == [REPORT_URI]


def zap_get_alerts_wrap(alert_dict):
    # ZAP passes on the dictionary this returns; an empty one counts as "no change", so the
    # dictionary is changed in place and returned either way.
    for plugin_id in list(alert_dict):
        kept = [a for a in alert_dict[plugin_id] if not only_report_uri(a)]
        if kept:
            alert_dict[plugin_id] = kept
        else:
            del alert_dict[plugin_id]
    return alert_dict
