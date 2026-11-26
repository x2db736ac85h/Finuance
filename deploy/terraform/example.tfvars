# A copy of this file, outside the repository or named *.tfvars here (ignored by git), gives the
# lab its values: terraform -chdir=deploy/terraform apply -var-file=lab.tfvars
# Every value below is a placeholder.
project = "example-project-1"
host    = "lab.example.org"
# the addresses that may open the lab: yours, and nobody else's (documentation ranges here)
allowed_cidrs = ["192.0.2.10/32"]
# region = "europe-west4" # the default; another region inside the EU is accepted
