# top-level comment
variable "region" {
  # inline comment about region
  type    = string
  default = "us-east-1" # trailing comment
}

resource "aws_instance" "web" {
  # this references the region variable
  region = var.region # <- here
}
