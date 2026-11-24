# Self-test of .zap/baseline_hooks.py (make check-zap-hooks): the report-uri notice is dropped, and
# every other CSP finding is kept. The alerts have the fields ZAP's API returns (pluginId, alertRef,
# other); the notice's text is the one of the weekly scan of 2026-11-23.
import importlib.util
import pathlib
import unittest

spec = importlib.util.spec_from_file_location(
    "baseline_hooks", pathlib.Path(__file__).with_name("baseline_hooks.py"))
hooks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hooks)

REPORT_URI = "Warnings:\nThe report-uri directive has been deprecated in favor of the new report-to directive\n"


def alert(ref, other="", plugin="10055"):
    return {"pluginId": plugin, "alertRef": ref, "other": other, "url": "http://localhost:8000/"}


class ReportURINotice(unittest.TestCase):
    def test_the_notice_alone_is_dropped(self):
        alerts = {"10055": [alert("10055-3", REPORT_URI)] * 3}
        out = hooks.zap_get_alerts_wrap(alerts)
        self.assertEqual(out, {})
        self.assertIs(out, alerts)  # changed in place: ZAP keeps its own dictionary for an empty one

    def test_other_csp_findings_are_kept(self):
        wildcard = alert("10055-4", "The following directives either allow wildcard sources ...")
        alerts = {"10055": [alert("10055-3", REPORT_URI), wildcard], "10038": [alert("10038-1", plugin="10038")]}
        self.assertEqual(hooks.zap_get_alerts_wrap(alerts), {"10055": [wildcard], "10038": [alert("10038-1", plugin="10038")]})

    def test_another_notice_is_kept(self):
        for other in (
            "Warnings:\nThe prefetch-src directive has been deprecated\n",
            REPORT_URI + "Errors:\nUnrecognised directive-name: plugin-types\n",
            "",
        ):
            with self.subTest(other=other):
                alerts = {"10055": [alert("10055-3", other)]}
                self.assertEqual(len(hooks.zap_get_alerts_wrap(alerts)["10055"]), 1)

    def test_the_same_text_in_another_alert_is_kept(self):
        alerts = {"10055": [alert("10055-1", REPORT_URI)]}
        self.assertEqual(len(hooks.zap_get_alerts_wrap(alerts)["10055"]), 1)


if __name__ == "__main__":
    unittest.main(verbosity=1)
