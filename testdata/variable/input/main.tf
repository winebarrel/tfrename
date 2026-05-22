variable "region" {
  type    = string
  default = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-123"
  instance_type = "t3.micro"
  tags = {
    region = var.region
    name   = "web-${var.region}"
  }
}

output "region" {
  value = var.region
}
