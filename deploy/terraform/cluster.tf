# One Autopilot cluster: pods are billed by what they ask for, and the node settings the lab would
# otherwise have to get right (shielded nodes, workload identity, the dataplane that enforces
# NetworkPolicies) are Google's defaults there.
resource "google_container_cluster" "lab" {
  name     = var.name
  location = var.region

  enable_autopilot = true
  # a lab is made to be removed again
  deletion_protection = false

  network    = google_compute_network.lab.id
  subnetwork = google_compute_subnetwork.lab.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  # No node has a public address. The control plane is reached through its DNS endpoint, where
  # Google checks the caller's identity (IAM), so there is no list of addresses to maintain.
  private_cluster_config {
    enable_private_nodes = true
  }
  control_plane_endpoints_config {
    dns_endpoint_config {
      allow_external_traffic = true
    }
    ip_endpoints_config {
      enabled = false
    }
  }

  # No address range may reach the control plane by address either: the list is on and empty.
  # (The address endpoint is switched off above; this keeps it closed if that ever changes.)
  master_authorized_networks_config {
  }

  # The nodes run as an identity of their own with the node's minimum role, not as the project's
  # default compute identity (which is an editor of the project unless someone changed that).
  cluster_autoscaling {
    auto_provisioning_defaults {
      service_account = google_service_account.nodes.email
    }
  }

  resource_labels = {
    app         = "finuance"
    environment = "lab"
  }

  release_channel {
    channel = "REGULAR"
  }

  # The Gateway API kinds and Google's gateway classes (the lab's front door is a Gateway).
  gateway_api_config {
    channel = "CHANNEL_STANDARD"
  }

  # Secrets come from Secret Manager, mounted by GKE's own add-on.
  secret_manager_config {
    enabled = true
  }

  depends_on = [google_project_service.lab]
}
