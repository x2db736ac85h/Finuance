# A network of the lab's own: nothing of the project's default network is used or changed.
resource "google_compute_network" "lab" {
  name                    = var.name
  auto_create_subnetworks = false
  depends_on              = [google_project_service.lab]
}

resource "google_compute_subnetwork" "lab" {
  name          = var.name
  region        = var.region
  network       = google_compute_network.lab.id
  ip_cidr_range = "10.80.0.0/20"

  # No VPC flow logs: they are billed by volume, and a lab that lives for hours has the cluster's
  # own logs and its NetworkPolicies' effect to look at. Switch them on here for an investigation.
  # Google's APIs (Vertex AI among them) are reached from the nodes' private addresses.
  private_ip_google_access = true

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = "10.84.0.0/14"
  }
  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = "10.88.0.0/20"
  }
}

# The nodes have no public addresses; this is their way out, for image pulls (ghcr.io, quay.io)
# and for the simulator's requests to the lab's own front door. It leaves from one reserved
# address, which Cloud Armor admits next to var.allowed_cidrs (edge.tf), so the simulator comes in
# as a browser would. The pods' own egress is decided by their NetworkPolicies, not here.
resource "google_compute_address" "nat" {
  name       = "${var.name}-nat"
  region     = var.region
  depends_on = [google_project_service.lab]
}

resource "google_compute_router" "lab" {
  name    = var.name
  region  = var.region
  network = google_compute_network.lab.id
}

resource "google_compute_router_nat" "lab" {
  name                               = var.name
  region                             = var.region
  router                             = google_compute_router.lab.name
  nat_ip_allocate_option             = "MANUAL_ONLY"
  nat_ips                            = [google_compute_address.nat.self_link]
  source_subnetwork_ip_ranges_to_nat = "ALL_SUBNETWORKS_ALL_IP_RANGES"

  log_config {
    enable = true
    filter = "ERRORS_ONLY"
  }
}

# Private Google Access with a name: *.googleapis.com answers with the four addresses of
# private.googleapis.com inside this network, so that a NetworkPolicy can allow "Google's APIs" as
# one small range (199.36.153.8/30) and nothing else on port 443.
resource "google_dns_managed_zone" "googleapis" {
  name        = "${var.name}-googleapis"
  dns_name    = "googleapis.com."
  description = "Google's APIs through Private Google Access, for the lab's network only."
  visibility  = "private"

  private_visibility_config {
    networks {
      network_url = google_compute_network.lab.id
    }
  }
  depends_on = [google_project_service.lab]
}

resource "google_dns_record_set" "googleapis_a" {
  managed_zone = google_dns_managed_zone.googleapis.name
  name         = "private.googleapis.com."
  type         = "A"
  ttl          = 300
  rrdatas      = ["199.36.153.8", "199.36.153.9", "199.36.153.10", "199.36.153.11"]
}

resource "google_dns_record_set" "googleapis_cname" {
  managed_zone = google_dns_managed_zone.googleapis.name
  name         = "*.googleapis.com."
  type         = "CNAME"
  ttl          = 300
  rrdatas      = ["private.googleapis.com."]
}
