# Parity report

Written by `web/e2e/parity.spec.ts` (PLAN task 14.3): the legacy single-file app and the new app on the same
data, compared in minor units. Differences listed in `testdata/oracle/ALLOWED_DIFFS.md` would be marked
there; any other difference fails the test.

Result: 30 of 30 checks equal.

| Path | Check | Legacy | New | Result |
|---|---|---|---|---|
| direct | All-time Σ | 4418254 | 4418254 | equal |
| direct | Summary all | {"spent":9701446,"income":14119700,"count":917} | {"spent":9701446,"income":14119700,"count":917} | equal |
| direct | Summary l3m | {"spent":1739659,"income":2811600,"count":155} | {"spent":1739659,"income":2811600,"count":155} | equal |
| direct | Summary m:2025-03 | {"spent":515182,"income":932300,"count":34} | {"spent":515182,"income":932300,"count":34} | equal |
| direct | Summary m:2025-02 | {"spent":556962,"income":958000,"count":50} | {"spent":556962,"income":958000,"count":50} | equal |
| direct | Summary m:2025-01 | {"spent":667515,"income":921300,"count":71} | {"spent":667515,"income":921300,"count":71} | equal |
| direct | Category totals | {"Artykuły spożywcze":1977141,"Bez kategorii":130647,"Mieszkanie":4275000,"Ogród":80230,"Rachunki":490496,"Restauracj… | {"Artykuły spożywcze":1977141,"Bez kategorii":130647,"Mieszkanie":4275000,"Ogród":80230,"Rachunki":490496,"Restauracj… | equal |
| direct | Cash flow per month | {"2024-01":{"income":950300,"expense":684086,"count":60},"2024-02":{"income":948900,"expense":636651,"count":63},"202… | {"2024-01":{"income":950300,"expense":684086,"count":60},"2024-02":{"income":948900,"expense":636651,"count":63},"202… | equal |
| direct | Recurring payments | [{"payee":"WSPÓLNOTA MIESZKANIOWA","median":285000,"count":15,"last":"2025-03-02","next":"2025-04-02"},{"payee":"DOST… | [{"payee":"WSPÓLNOTA MIESZKANIOWA","median":285000,"count":15,"last":"2025-03-02","next":"2025-04-02"},{"payee":"DOST… | equal |
| direct | Overview 2025-03 | {"income":932300,"expense":515182,"count":34} | {"income":932300,"expense":515182,"count":34} | equal |
| direct | Overview 2025-02 | {"income":958000,"expense":556962,"count":50} | {"income":958000,"expense":556962,"count":50} | equal |
| direct | Overview 2025-01 | {"income":921300,"expense":667515,"count":71} | {"income":921300,"expense":667515,"count":71} | equal |
| direct | Final balance | 4418254 | 4418254 | equal |
| migration | All-time Σ | 4930014 | 4930014 | equal |
| migration | Summary all | {"spent":10039686,"income":14969700,"count":925} | {"spent":10039686,"income":14969700,"count":925} | equal |
| migration | Summary l3m | {"spent":2077899,"income":3661600,"count":163} | {"spent":2077899,"income":3661600,"count":163} | equal |
| migration | Summary m:2025-03 | {"spent":515182,"income":1782300,"count":35} | {"spent":515182,"income":1782300,"count":35} | equal |
| migration | Summary m:2025-02 | {"spent":871762,"income":958000,"count":54} | {"spent":871762,"income":958000,"count":54} | equal |
| migration | Summary m:2025-01 | {"spent":690955,"income":921300,"count":74} | {"spent":690955,"income":921300,"count":74} | equal |
| migration | Category totals | {"Artykuły spożywcze":1989141,"Bez kategorii":139547,"Mieszkanie":4560000,"Ogród":80230,"Prezenty":8900,"Rachunki":49… | {"Artykuły spożywcze":1989141,"Bez kategorii":139547,"Mieszkanie":4560000,"Ogród":80230,"Prezenty":8900,"Rachunki":49… | equal |
| migration | Cash flow per month | {"2024-01":{"income":950300,"expense":684086,"count":60},"2024-02":{"income":948900,"expense":636651,"count":63},"202… | {"2024-01":{"income":950300,"expense":684086,"count":60},"2024-02":{"income":948900,"expense":636651,"count":63},"202… | equal |
| migration | Recurring payments | [{"payee":"WSPÓLNOTA MIESZKANIOWA","median":285000,"count":15,"last":"2025-03-02","next":"2025-04-02"},{"payee":"DOST… | [{"payee":"WSPÓLNOTA MIESZKANIOWA","median":285000,"count":15,"last":"2025-03-02","next":"2025-04-02"},{"payee":"DOST… | equal |
| migration | Overview 2025-03 | {"income":1782300,"expense":515182,"count":35} | {"income":1782300,"expense":515182,"count":35} | equal |
| migration | Overview 2025-02 | {"income":958000,"expense":871762,"count":54} | {"income":958000,"expense":871762,"count":54} | equal |
| migration | Overview 2025-01 | {"income":921300,"expense":690955,"count":74} | {"income":921300,"expense":690955,"count":74} | equal |
| migration | Final balance | 5080014 | 5080014 | equal |
| migration | Group totals | {"Remont":{"count":3,"spent":14063},"Wakacje nad morzem":{"count":5,"spent":49675}} | {"Remont":{"count":3,"spent":14063},"Wakacje nad morzem":{"count":5,"spent":49675}} | equal |
| migration | Duplicates skipped | 3 | 3 | equal |
| migration | Budgets | {"Artykuły spożywcze":150000,"Rachunki":40000,"Sport":10000} | {"Artykuły spożywcze":150000,"Rachunki":40000,"Sport":10000} | equal |
| migration | Investments | {"ppkValue":2540000,"positions":[{"name":"ETF World","invested":4000000,"value":4650000}]} | {"ppkValue":2540000,"positions":[{"name":"ETF World","invested":4000000,"value":4650000}]} | equal |
