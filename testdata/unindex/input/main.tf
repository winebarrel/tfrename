resource "aws_instance" "foo" {
  count         = 1
  ami           = "ami-123"
  instance_type = "t3.micro"
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo[0].id
}

output "ip" {
  value = aws_instance.foo[0].public_ip
}

# Non-matching: different name, different index, splat.
output "other_name" {
  value = aws_instance.bar[0].id
}

output "other_index" {
  value = aws_instance.foo[1].id
}

output "splat" {
  value = aws_instance.foo[*].id
}
