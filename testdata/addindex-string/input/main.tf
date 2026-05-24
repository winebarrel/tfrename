resource "zoo_thing" "baz" {
  name = "single"
}

output "id" {
  value = zoo_thing.baz.id
}

output "name" {
  value = zoo_thing.baz.name
}
