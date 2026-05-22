locals {
  region = "us-east-1"
  name   = "web-${local.region}"
}

resource "aws_instance" "web" {
  ami           = "ami-123"
  instance_type = "t3.micro"
  tags = {
    Name   = local.name
    Region = local.region
  }
}
