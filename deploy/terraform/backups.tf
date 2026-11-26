# The database's backups in Cloud Storage, for the restore drill (PLAN 27.9). Off unless
# var.backups: a lab is made again rather than restored, so a run needs none until the drill.
#
# On, the bucket is the lab's alone: in its region, no public access, one access model, no soft
# delete (deleted objects would be billed for another week), and emptied with the lab
# (force_destroy: nothing of a lab outlives `make gcp-down`). The database's instances write to it
# with an identity of their own, bound by workload identity to the Kubernetes service account
# CloudNativePG makes for the cluster; the identity may use this bucket's objects and nothing else.
# No key (deploy/k8s/db: objectStore.auth gke).
locals {
  # CloudNativePG names the instances' service account after the cluster (deploy/k8s/db cluster.name)
  db_service_account = "finuance-db"
}

# bucket names are global: a suffix keeps two labs (or a lab made again) apart
resource "random_id" "backups" {
  count       = var.backups ? 1 : 0
  byte_length = 4
}

resource "google_storage_bucket" "backups" {
  count                       = var.backups ? 1 : 0
  name                        = "${var.name}-backups-${random_id.backups[0].hex}"
  location                    = var.region
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = true
  labels                      = { app = "finuance", environment = "lab" }

  soft_delete_policy {
    retention_duration_seconds = 0
  }
  depends_on = [google_project_service.lab]
}

resource "google_service_account" "db" {
  count        = var.backups ? 1 : 0
  account_id   = "${var.name}-db"
  display_name = "Finuance lab: the database's backups"
  depends_on   = [google_project_service.lab]
}

resource "google_storage_bucket_iam_member" "db" {
  count  = var.backups ? 1 : 0
  bucket = google_storage_bucket.backups[0].name
  role   = "roles/storage.objectAdmin"
  member = google_service_account.db[0].member
}

resource "google_service_account_iam_member" "db_workload_identity" {
  count              = var.backups ? 1 : 0
  service_account_id = google_service_account.db[0].name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project}.svc.id.goog[${var.namespaces.lab}/${local.db_service_account}]"
  depends_on         = [google_container_cluster.lab]
}
