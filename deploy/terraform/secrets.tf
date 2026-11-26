# The lab's two secrets: the simulator's seed (its personas' passwords are derived from it) and
# its control token. Their values are made here and written to Secret Manager without ever being
# stored: an ephemeral value exists only while Terraform runs, and a write-only argument is sent
# to the provider and kept out of the plan and the state. So the values are in neither git nor
# the state bucket; whoever needs one reads it from Secret Manager with their own identity.
ephemeral "random_password" "seed" {
  length  = 48
  special = false
}

ephemeral "random_password" "control" {
  length  = 48
  special = false
}

resource "google_secret_manager_secret" "seed" {
  secret_id = "${var.name}-sim-seed"
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
  depends_on = [google_project_service.lab]
}

resource "google_secret_manager_secret" "control" {
  secret_id = "${var.name}-sim-control"
  replication {
    user_managed {
      replicas {
        location = var.region
      }
    }
  }
  depends_on = [google_project_service.lab]
}

resource "google_secret_manager_secret_version" "seed" {
  secret                 = google_secret_manager_secret.seed.id
  secret_data_wo         = ephemeral.random_password.seed.result
  secret_data_wo_version = 1 # raise it to make a new value
}

resource "google_secret_manager_secret_version" "control" {
  secret                 = google_secret_manager_secret.control.id
  secret_data_wo         = ephemeral.random_password.control.result
  secret_data_wo_version = 1
}
