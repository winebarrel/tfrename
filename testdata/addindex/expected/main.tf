resource "aws_instance" "foo" {
  ami           = "ami-123"
  instance_type = var.size
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo[0].id
}

output "ip" {
  value = aws_instance.foo[0].public_ip
}

# Non-matching: different name, different type.
output "other_name" {
  value = aws_instance.bar.id
}

output "other_type" {
  value = aws_eip.addr.id
}
