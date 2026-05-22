resource "aws_instance" "web" {
  tags = {
    Env = var.env
  }
}
