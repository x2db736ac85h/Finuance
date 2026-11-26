output "address" {
  description = "Point the lab's DNS record (an A record for var.host) at this address; the certificate is issued once it resolves."
  value       = google_compute_global_address.lab.address
}

output "cluster" {
  description = "How to reach the cluster: gcloud container clusters get-credentials <name> --region <region> --dns-endpoint"
  value = {
    name   = google_container_cluster.lab.name
    region = var.region
  }
}

output "lab_values" {
  description = "What deploy/environments/gke-lab leaves as placeholders: the values an overlay or --set gives the charts."
  value = {
    project                  = var.project
    host                     = var.host
    service_account          = google_service_account.lab.email
    security_policy          = google_compute_security_policy.lab.name
    certificate_map          = google_certificate_manager_certificate_map.lab.name
    address_name             = google_compute_global_address.lab.name
    seed_secret              = google_secret_manager_secret.seed.secret_id
    control_secret           = google_secret_manager_secret.control.secret_id
    vertex_location          = var.region
    google_apis_egress_range = "199.36.153.8/30"
  }
}
