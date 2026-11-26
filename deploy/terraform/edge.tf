# The front door: one address, one certificate, and a policy that lets the maintainer's addresses
# in and nobody else. The Gateway that uses them is a Kubernetes object (deploy/environments/
# gke-lab/platform), applied after the cluster exists.
resource "google_compute_global_address" "lab" {
  name       = var.name
  depends_on = [google_project_service.lab]
}

# A Google-managed certificate for the lab's host name. It is issued once the name's DNS record
# points at the address above, which is the maintainer's one step by hand (docs/gcp.md).
resource "google_certificate_manager_certificate" "lab" {
  name = var.name

  managed {
    domains = [var.host]
  }
  depends_on = [google_project_service.lab]
}

resource "google_certificate_manager_certificate_map" "lab" {
  name       = var.name
  depends_on = [google_project_service.lab]
}

resource "google_certificate_manager_certificate_map_entry" "lab" {
  name         = var.name
  map          = google_certificate_manager_certificate_map.lab.name
  certificates = [google_certificate_manager_certificate.lab.id]
  hostname     = var.host
}

# Cloud Armor: the allowed ranges, then everything else refused. The chart attaches it to both
# backends (gateway.gke.securityPolicy).
resource "google_compute_security_policy" "lab" {
  name        = var.name
  description = "The lab is for its maintainer: the allowed addresses only."

  rule {
    action      = "allow"
    priority    = 1000
    description = "The addresses of var.allowed_cidrs"
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = var.allowed_cidrs
      }
    }
  }

  rule {
    action      = "deny(403)"
    priority    = 2147483647
    description = "Everyone else"
    match {
      versioned_expr = "SRC_IPS_V1"
      config {
        src_ip_ranges = ["*"]
      }
    }
  }
  depends_on = [google_project_service.lab]
}
