variable "region" {
  default = "us-east-1"
}

resource "aws_instance" "web" {
  region = var.region
}
