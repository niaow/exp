name Math
desc "Math is a system to do math."

op Add {
    desc "Adds two numbers."
    encoding query
    in X {
        type uint32
        desc "X is the first number."
    }
    in Y {
        type uint32
        desc "Y is the second number."
    }
    out Sum {
        type uint32
        desc "Sum is the sum of the two numbers."
    }
}


op Divide {
    desc "Divides two numbers."
    encoding json
    in X {
        type uint32
        desc "X is the dividend."
    }
    in Y {
        type uint32
        desc "Y is the divisor."
    }
    out Quotient {
        type uint32
        desc "Quotient is the quotient of the division."
    }
    out Remainder {
        type uint32
        desc "Remainder is the remainder of the division."
    }
    err ErrDivideByZero
}

err ErrDivideByZero {
    desc "ErrDivideByZero is an error resulting from a division with a zero divisor."
    text "division by zero"
    field Dividend {
        type uint32
        desc "Dividend is the dividend of the erroneous division."
    }
    code 400
}


type Stats struct {
    Mean float64 { desc "Mean is the average of the data in the set" }
    Stdev float64 { desc "Stdev is the standard deviation of the data in the set" }
} "Stats is a set of summative statistics."

op Statistics {
    desc "Statistics calculates summative statistics for a set of data"
    encoding json
    in Data []float64 {
        desc "Data is the data set to be summarized"
    }
    out Results Stats {
        desc "Results are the resulting summary statistics."
    }
    err ErrNoData
}

err ErrNoData {
    desc "ErrNoData is an error indicating that no data was provided to summarize."
    text "no data provided"
    code 400
}
