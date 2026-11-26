# The lab's one Google identity: what the api and the worker are when they ask Vertex AI for an
# answer. It can use Vertex AI and nothing else, and it has no key: the pods get short-lived tokens
# from the metadata server because their Kubernetes service account is bound to it (workload
# identity, ADR 0045). No google_service_account_key exists anywhere in this configuration, and
# tests/lab.tftest.hcl keeps it that way.
resource "google_service_account" "lab" {
  account_id   = var.name
  display_name = "Finuance lab: the assistant's access to Vertex AI"
  depends_on   = [google_project_service.lab]
}

resource "google_project_iam_member" "vertex" {
  project = var.project
  role    = "roles/aiplatform.user"
  member  = google_service_account.lab.member
}

resource "google_service_account_iam_member" "workload_identity" {
  service_account_id = google_service_account.lab.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project}.svc.id.goog[${var.namespaces.lab}/${var.namespaces.service_account}]"
  depends_on         = [google_container_cluster.lab]
}

# What the cluster's nodes are: an identity that can write logs and metrics and pull images, and
# no more (the role Google keeps for exactly that).
resource "google_service_account" "nodes" {
  account_id   = "${var.name}-nodes"
  display_name = "Finuance lab: the cluster's nodes"
  depends_on   = [google_project_service.lab]
}

resource "google_project_iam_member" "nodes" {
  project = var.project
  role    = "roles/container.defaultNodeServiceAccount"
  member  = google_service_account.nodes.member
}

