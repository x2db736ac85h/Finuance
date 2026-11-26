# The services the lab uses. Switching one on costs nothing by itself. They are left on when the
# lab is destroyed: switching a service off can break something else in the project.
locals {
  services = [
    "compute.googleapis.com",
    "container.googleapis.com",
    "dns.googleapis.com",
    "certificatemanager.googleapis.com",
    "secretmanager.googleapis.com",
    "aiplatform.googleapis.com",
    "iamcredentials.googleapis.com",
  ]
}

resource "google_project_service" "lab" {
  for_each           = toset(local.services)
  service            = each.value
  disable_on_destroy = false
}
