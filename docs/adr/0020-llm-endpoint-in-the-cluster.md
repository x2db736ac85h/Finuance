# 0020. Where the assistant's model runs when Finuance is on Kubernetes

- Status: accepted (the address is set in Phase 18)
- Date: 2026-10-24

## Context

ADR 0015 made the assistant provider-agnostic: the instance lists named connections in a connections file, and users pick one of them and a model. The maintainer chose **LM Studio** for dev and prod (Phase 13). On Kubernetes (PLAN 16.8) two questions remain:

- Where does the model server run?
- How does the api reach it, given that the chart denies all egress by default?

Constraints:

- The homelab cluster is small (k3s, no GPU node known).
- LM Studio runs on a desktop machine with a GPU, reachable over the LAN or the tailnet.
- The api's private-address check only lets it reach loopback, RFC 1918, ULA, link-local and CGNAT addresses (the tailnet's 100.64.0.0/10), unless a connection is marked `cloud`.
- The NetworkPolicy must allow exactly the model server and nothing else.
- Finuance must keep working when the model server is off (the assistant then reports itself unavailable).

## Decision

1. **The model stays outside the cluster: LM Studio on the GPU machine, reached over the LAN or the tailnet.** The chart gets it as an ordinary connection:

   ```yaml
   assistant:
     enabled: true
     connectionsFile: |
       {"connections":[{"name":"lmstudio","label":"LM Studio","api":"openai",
         "baseUrl":"http://<address>:1234/v1","defaultModel":"<model>"}]}
     egress:
       - cidr: <address>/32
         ports: [1234]
   ```

   `<address>` is a LAN address (`192.168.x.y`) or the machine's tailnet address (`100.x.y.z`). Both pass the private-address check. The NetworkPolicy allows the api, and only the api, to reach that one address and port. A host name works in `baseUrl` (the check resolves it), but the policy needs the address, so a fixed address (a DHCP reservation, or the tailnet's stable `100.x` address) is required.

2. **The tailnet is preferred over the LAN** when the cluster nodes are on it. The connection survives a change of LAN, and Tailscale encrypts it, so the plain-HTTP API of LM Studio is not exposed on the LAN. If the nodes aren't on the tailnet, the LAN address is used, and LM Studio's server should listen only on that interface.

3. **An in-cluster model server stays possible without code changes.** For Ollama or vLLM in their own namespace, the connection's `baseUrl` is the Service (`http://ollama.ollama.svc:11434/v1`), and the egress entry is a peer instead of a CIDR:

   ```yaml
   egress:
     - to: [{ namespaceSelector: { matchLabels: { kubernetes.io/metadata.name: ollama } } }]
       ports: [11434]
   ```

   The CI rehearsal uses exactly this shape for the mock model server (`deploy/k8s/rehearsal/app-values.yaml`). It is the option for a cluster that later gets a GPU node.

4. **Cloud connections** (OpenAI, Anthropic) stay possible as in ADR 0015: `cloud: true`, an API key from the `finuance-assistant` sealed secret, and an egress entry for the provider. A CIDR doesn't fit a provider's changing addresses, so that egress is `0.0.0.0/0` on 443 for the api only. That is a deliberate widening, documented next to the setting, and not part of the homelab default.

5. **The choice of address, model and toolset is made in Phase 18** with the endpoint at hand (`finuance admin llm-probe` and `make eval-assistant` on the real model, Phase 13 follow-up). Until then the homelab values keep `assistant.enabled: false`.

## Consequences (positive, negative, follow-ups)

- The cluster doesn't need a GPU or the memory a model takes, and the model can be swapped in LM Studio without touching the cluster.
- The assistant depends on a machine outside the cluster being on. When it's off, `GET /assistant/status` reports the connection as unreachable and the panel says so; the rest of the app is unaffected.
- The egress rule is one address and one port: the api can't use the assistant path to reach anything else (PLAN §8.1, SSRF / data egress).
- LM Studio's API has no authentication. Anything that can reach its port can use the model. Keep it to the tailnet, or to a LAN interface with a firewall rule for the cluster nodes.
- Follow-ups (Phase 18): pick the address (tailnet or LAN) and make it fixed; set the connections file and the egress in `deploy/environments/homelab/values.yaml`; run the probe and the evals; record the default model and toolset in `docs/evals.md`.

## Alternatives considered

- **Ollama in the cluster now**: the homelab nodes have no GPU known to us. CPU inference of a tool-calling 7–14B model is too slow for the assistant's multi-round tool loop. Kept as option 3 for later.
- **An egress proxy** (e.g. Squid with an allow-list) in front of the model server: one more component to run, for one fixed address that a NetworkPolicy already pins.
- **A Kubernetes Service without selectors (or an ExternalName) for the LM Studio machine**: gives a cluster DNS name, but NetworkPolicies still match the IP, and ExternalName targets are resolved outside the policy model. It adds indirection without adding control.
- **Cloud model by default**: the user's transactions would leave the homelab. ADR 0015 requires an admin flag and per-user consent for that, so it can't be the default.
