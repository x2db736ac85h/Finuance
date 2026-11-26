variable "project" {
  description = "The Google Cloud project the lab is made in. It needs billing; nothing else of it is assumed."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{4,28}[a-z0-9]$", var.project))
    error_message = "A project id is 6 to 30 lower-case letters, digits and hyphens."
  }
}

variable "region" {
  description = "Where the cluster runs and where Vertex AI processes questions. A region inside the EU keeps both there."
  type        = string
  default     = "europe-west4"

  validation {
    condition     = can(regex("^europe-", var.region))
    error_message = "The lab is planned for a region inside the EU (europe-…); change this rule on purpose to leave it."
  }
}

variable "name" {
  description = "The name every resource of the lab starts with."
  type        = string
  default     = "finuance-lab"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,20}$", var.name))
    error_message = "A short lower-case name."
  }
}

variable "host" {
  description = "The lab's host name (lab.example.org). Its DNS record is the maintainer's to set, to the address in the outputs."
  type        = string

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,}$", var.host))
    error_message = "A fully qualified host name in lower case."
  }
}

variable "allowed_cidrs" {
  description = "The addresses that may reach the lab through the load balancer (Cloud Armor). Everything else is refused. Never 0.0.0.0/0: a lab is not a public service."
  type        = list(string)

  validation {
    condition     = length(var.allowed_cidrs) > 0 && length(var.allowed_cidrs) <= 10
    error_message = "One to ten ranges."
  }
  validation {
    condition     = alltrue([for c in var.allowed_cidrs : can(cidrhost(c, 0)) && !contains(["0.0.0.0/0", "::/0"], c)])
    error_message = "Each entry is a CIDR range, and none of them is the whole internet."
  }
}

variable "backups" {
  description = "A Cloud Storage bucket for the database's backups and the identity that writes to it (backups.tf), for the restore drill. Off: a lab is made again rather than restored."
  type        = bool
  default     = false
}

variable "namespaces" {
  description = "The Kubernetes namespaces of the lab: the instance's, and the service account in it that may use Vertex AI."
  type = object({
    lab             = string
    service_account = string
  })
  default = {
    lab             = "finuance-lab"
    service_account = "finuance"
  }
}
