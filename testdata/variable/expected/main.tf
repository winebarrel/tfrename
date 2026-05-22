variable "aws_region" {
  type    = string
  default = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-123"
  instance_type = "t3.micro"
  tags = {
    region = var.aws_region
    name   = "web-${var.aws_region}"
  }
}

output "region" {
  value = var.aws_region
}
