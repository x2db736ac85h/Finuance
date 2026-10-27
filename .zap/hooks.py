# ZAP hook for the API scan (tools/zap-scan.sh): signs every request in as the scan's own user
# (session cookie and the CSRF headers the API requires). The operations that would end that
# session are left out of the scanned spec by tools/zap-scan.sh.
import os


def zap_started(zap, target):
    base = os.environ["ZAP_BASE_URL"]
    headers = {
        "Cookie": "finuance_session=" + os.environ["ZAP_SESSION"],
        "X-Finuance-Client": "web",
        "Origin": base,
    }
    for name, value in headers.items():
        zap.replacer.add_rule(description="finuance " + name, enabled="true", matchtype="REQ_HEADER",
                              matchregex="false", matchstring=name, replacement=value)
