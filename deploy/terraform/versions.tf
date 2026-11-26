# The Google Cloud lab (PLAN Phase 27, docs/gcp.md): everything that costs money or holds an
# identity is made here and removed again with `terraform destroy` (make gcp-up, make gcp-down).
# Hand-written resources, no module: one small cluster does not need one.
#
# Written so that OpenTofu runs it too: nothing here is specific to one of the two.

terraform {
  required_version = ">= 1.11.0" # write-only arguments and ephemeral resources (secrets.tf)

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "8.12.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "3.9.1"
    }
  }

  # The state lives in a bucket the maintainer makes once, by hand (docs/gcp.md): it is the one
  # thing that must exist before and after the lab. Its name is given at `terraform init`
  # (-backend-config="bucket=…"), so that no project's name is in this repository.
  backend "gcs" {
    prefix = "finuance-lab"
  }
}

provider "google" {
  project = var.project
  region  = var.region
}
