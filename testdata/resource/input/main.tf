resource "aws_instance" "foo" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}

resource "aws_eip" "addr" {
  instance = aws_instance.foo.id
}

output "ip" {
  value = aws_instance.foo.public_ip
}
