# Observability

What the api and the worker tell an operator, and how the three signals are joined: **logs** (one
JSON object per line on standard error), **metrics** (Prometheus, on the admin port) and **traces**
(OpenTelemetry, sent over OTLP when an endpoint is configured). The service-level objectives and
their alerts are in [SLO.md](SLO.md); the dashboards, rules and how to reach Grafana are in
[RUNBOOK.md](RUNBOOK.md).

No signal carries what a person typed, what a model wrote or what a tool returned: no bodies, no
query strings, no statement parameters, no prompt or answer text. A user appears as a hash
([THREAT-MODEL.md](THREAT-MODEL.md), boundary B9 and row I20).

## Log fields

In production every line is one JSON object. The fields below mean the same on every line; the
list is `observability.LogFields` in the code, and a test keeps this table and that list in step.
A line may add fields of its own (`count`, `import`, `error`, `connection`, …) but never uses one
of these names for something else.

| Field | On which lines | Meaning |
|---|---|---|
| `time` | all | when the line was written (RFC 3339, with nanoseconds) |
| `level` | all | `DEBUG`, `INFO`, `WARN` or `ERROR` |
| `msg` | all | a fixed sentence per call site, never built from data; `request` is the access line |
| `request_id` | everything written while a request is served | the id the response carries in `X-Request-ID` and a problem response in `requestId` |
| `trace_id` | everything written inside a span: a request, a job, an assistant run | the OpenTelemetry trace id; the same id finds the trace in Tempo |
| `span_id` | as `trace_id` | the span that was current when the line was written |
| `user` | the access line of a signed-in request, the assistant's run line | the first 8 bytes of SHA-256 of the user id, in hex; never the id or the e-mail address |
| `route` | the access line | the route pattern (`GET /api/v1/imports/{id}`), never the path with its values; `unmatched` for a 404 |
| `method` | the access line | the HTTP method |
| `status` | the access line | the status sent; 500 when the handler panicked before sending anything |
| `bytes` | the access line | the size of the response body |
| `duration_ms` | the access line, the assistant's run line | how long it took |
| `client_ip` | the access line | the client's address as the trusted proxies report it |
| `job_id` | every line a job writes | River's id of the job |
| `kind` | every line a job writes | the job's kind (`commit_import`, `reclassify`, …) |
| `attempt` | every line a job writes | which attempt this is, from 1 |

A line gets `trace_id` and `span_id` from the context it is logged with, so code logs with its
context (`InfoContext` and friends; the linter requires it where a context is in scope). Errors
logged as values are written as their class only: for a database error the SQLSTATE, constraint,
table and column, never its message.

## Following one request

1. **From what the user saw.** The response header `X-Request-ID` (and `requestId` in a problem
   response) is the `request_id` of every line the request wrote.
2. **To its lines.** The access line (`msg` = `request`) has the route, status and duration; a
   `request failed` or `panic` line with the same `request_id` says what went wrong.
3. **To its trace.** Any of those lines has the `trace_id`. The trace shows the request, its
   database statements and, when the request queued a job, the job as a child span.
4. **To the job's lines.** The job's lines carry the same `trace_id` and their own `job_id`,
   `kind` and `attempt`.

The kubelet's probes (`/healthz`, `/readyz`) are not traced, and their access lines are written at
debug level only; a failing probe is an error line like any other failing request. A handler that
panics is one access line with status 500, one `panic` line with the stack, one 5xx in
`http_server_requests_total`, one in `finuance_panics_total`, and the alert `FinuancePanic`.

## Traces

A request is one trace: the server span named after the route pattern, its database statements
(without their text), and below it whatever the request started.

| Span | Kind | What it covers | Its own attributes |
|---|---|---|---|
| the route pattern, e.g. `POST /api/v1/imports/{id}/commit` | server | one request; always a new trace, a client's `traceparent` is only kept as a link | the HTTP method, route and status |
| `job <kind>` | consumer | one attempt of a job, as a child of the request that queued it | `river.kind`, `river.job_id`, `river.attempt`, `river.queue` |
| `assistant.run` | internal | one question to the assistant | `assistant.mode`, `assistant.result`, `assistant.rounds` |
| `llm.request` | client | one request to a model server | `gen_ai.operation.name`, `gen_ai.provider.name`, `gen_ai.request.model`, `gen_ai.response.time_to_first_chunk`, `gen_ai.usage.input_tokens` and `gen_ai.usage.output_tokens` when the server reports them, `error.type`, `assistant.connection`, `assistant.round` |
| `assistant.tool` | internal | one tool call | `assistant.tool` (the tool's name, or `unknown`) |

The `gen_ai.*` names follow the OpenTelemetry GenAI conventions, which were still in development
when they were last checked (2026-11-20); they may be renamed. No span carries a prompt, an answer,
a tool's arguments or its result, and no trace context is sent to a model server.

Every span's resource names the service (`service.name`: `finuance-api` or `finuance-worker`), its
version, the environment (`deployment.environment.name`) and the instance (`service.instance.id`).
On Kubernetes the chart adds `k8s.pod.name` and `k8s.namespace.name` from the pod's own fields, and
`otel.resourceAttributes` in the values adds more.

`FINUANCE_OTEL_SAMPLE_RATIO` (the chart's `otel.sampleRatio`; default 1) is the share of requests
that are traced. The decision is made once, at the request: a trace is kept whole or not at all,
and a job follows the request that queued it.

## Metrics

The api and the worker serve Prometheus metrics on their admin port (`/metrics`, never through the
ingress). The full list is `deploy/k8s/observability/metrics.txt`, kept in step with the code by a
test. Labels are bounded: a route is its pattern, a job its kind, and no label carries a user.

| Metric | Labels | What it says |
|---|---|---|
| `http_server_requests_total` | `route`, `method`, `status_class` | every request, a panicking one included |
| `http_server_request_duration_seconds` | `route` | how long requests take; each bucket carries one request's trace id as an exemplar |
| `finuance_panics_total` | | handlers that panicked; any increase raises `FinuancePanic` |
| `finuance_rate_limited_total` | `limiter` | requests the api's own limiters refused, by limiter (`login_ip`, `login_failed`, `upload`, `assistant`, …) |
| `finuance_auth_failures_total` | `reason` | failed sign-ins: `password` or `second_step` |
| `finuance_jobs_total`, `finuance_job_duration_seconds` | `kind`, `result` | the worker's jobs |
| `finuance_imports_total`, `finuance_import_rows_total` | `result`; `kind` | committed imports and their rows |
| `finuance_assistant_runs_total`, `…_run_duration_seconds` | `mode`, `result` | questions to the assistant |
| `finuance_assistant_model_requests_total` | `connection`, `result` | requests to model servers and how they ended |
| `finuance_llm_request_duration_seconds` | `connection` | one request to a model server, to the end of its answer |
| `finuance_llm_time_to_first_token_seconds` | `connection` | until the first piece of the answer arrives |
| `finuance_llm_tokens_total` | `connection`, `direction` | tokens as the model servers report them (`input`, `output`) |

The assistant's metrics come from both processes: the api answers chats and the worker writes the
digests, so their dashboard and the alert `FinuanceAssistantModelErrors` read both jobs.

**Exemplars.** A scraper that asks for the OpenMetrics format gets, with each latency bucket, the
trace id of one request that fell into it. With `exemplar-storage` switched on in Prometheus and the
Tempo link on Grafana's Prometheus data source (both set in the reference environment's monitoring
values), the dots on "p95 by route" open that request's trace.
