resource "aws_instance" "web" {
  tags = {
    Env = var.environment
  }
}
