# What the lab must always be, checked on a plan with made-up providers: no Google account, no
# network. `terraform test` runs it in CI (tools/check-terraform.sh).

mock_provider "google" {}
mock_provider "random" {}

variables {
  project       = "example-project-1"
  host          = "lab.example.org"
  allowed_cidrs = ["192.0.2.10/32", "198.51.100.0/24"]
}

run "the_lab_as_planned" {
  command = plan

  assert {
    condition     = google_container_cluster.lab.enable_autopilot == true
    error_message = "The lab is an Autopilot cluster."
  }
  assert {
    condition     = google_container_cluster.lab.deletion_protection == false
    error_message = "A lab that can't be destroyed would be left behind (and billed)."
  }
  assert {
    condition     = google_container_cluster.lab.private_cluster_config[0].enable_private_nodes == true
    error_message = "The nodes have no public addresses."
  }
  assert {
    condition     = google_container_cluster.lab.control_plane_endpoints_config[0].ip_endpoints_config[0].enabled == false
    error_message = "The control plane is reached through its DNS endpoint with IAM, not by address."
  }
  assert {
    condition     = length(google_container_cluster.lab.master_authorized_networks_config) == 1 && length(google_container_cluster.lab.master_authorized_networks_config[0].cidr_blocks) == 0
    error_message = "The list of networks that may reach the control plane by address is on and empty."
  }
  assert {
    condition     = google_container_cluster.lab.location == "europe-west4"
    error_message = "The cluster is in the planned EU region unless var.region says otherwise."
  }
  assert {
    condition     = google_compute_subnetwork.lab.private_ip_google_access == true
    error_message = "Google's APIs are reached privately."
  }
  assert {
    condition     = google_compute_network.lab.auto_create_subnetworks == false
    error_message = "The lab's network has only the subnet made here."
  }

  assert {
    condition     = google_project_iam_member.nodes.role == "roles/container.defaultNodeServiceAccount"
    error_message = "The nodes' identity has the node's minimum role only."
  }
  # the identity for Vertex AI: one role, bound to one Kubernetes service account
  assert {
    condition     = google_project_iam_member.vertex.role == "roles/aiplatform.user"
    error_message = "The lab's identity may use Vertex AI and nothing else."
  }
  assert {
    condition     = google_service_account_iam_member.workload_identity.member == "serviceAccount:example-project-1.svc.id.goog[finuance-lab/finuance]"
    error_message = "Only the lab instance's service account acts as the Google identity."
  }

  # the front door: the allowed addresses, then nobody
  assert {
    condition = toset([for r in google_compute_security_policy.lab.rule : "${r.priority} ${r.action}"]) == toset([
      "1000 allow", "2147483647 deny(403)",
    ])
    error_message = "The policy allows the given ranges and refuses everything else, and has no other rule."
  }
  assert {
    condition = one([for r in google_compute_security_policy.lab.rule : r.match[0].config[0].src_ip_ranges if r.priority == 1000]) == toset([
      "192.0.2.10/32", "198.51.100.0/24",
    ])
    error_message = "The allow rule holds exactly var.allowed_cidrs."
  }
  assert {
    condition     = google_certificate_manager_certificate.lab.managed[0].domains == tolist(["lab.example.org"])
    error_message = "The certificate is for the lab's host name alone."
  }

  # Private Google Access by name: the one range a NetworkPolicy allows
  assert {
    condition     = toset(google_dns_record_set.googleapis_a.rrdatas) == toset(["199.36.153.8", "199.36.153.9", "199.36.153.10", "199.36.153.11"])
    error_message = "*.googleapis.com resolves to private.googleapis.com's four addresses."
  }
  assert {
    condition     = google_dns_managed_zone.googleapis.visibility == "private"
    error_message = "The googleapis.com zone is seen by the lab's network only."
  }

  # the services stay on when the lab goes: switching one off can break something else
  assert {
    condition     = alltrue([for s in google_project_service.lab : s.disable_on_destroy == false])
    error_message = "Destroying the lab must not switch a service of the project off."
  }
}

# The refusals: a lab is never open to the internet, never outside the EU by accident.
run "not_the_whole_internet" {
  command = plan
  variables {
    allowed_cidrs = ["0.0.0.0/0"]
  }
  expect_failures = [var.allowed_cidrs]
}

run "not_an_empty_allowlist" {
  command = plan
  variables {
    allowed_cidrs = []
  }
  expect_failures = [var.allowed_cidrs]
}

run "not_outside_the_eu_by_accident" {
  command = plan
  variables {
    region = "us-central1"
  }
  expect_failures = [var.region]
}

run "a_real_host_name" {
  command = plan
  variables {
    host = "Lab_Example"
  }
  expect_failures = [var.host]
}
