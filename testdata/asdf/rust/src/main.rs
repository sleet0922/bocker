fn checksum(values: &[u32]) -> u32 {
    values.iter().sum()
}

fn main() {
    println!(
        "{{\"language\":\"rust\",\"version\":\"1.89.0\",\"checksum\":{}}}",
        checksum(&[2, 3, 5, 7, 11])
    );
}

#[cfg(test)]
mod tests {
    use super::checksum;

    #[test]
    fn calculates_checksum() {
        assert_eq!(checksum(&[2, 3, 5, 7, 11]), 28);
    }
}
