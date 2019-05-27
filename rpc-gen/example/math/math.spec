name Math;
desc "Math is a system to do math.";

op Add {
    desc "Adds two numbers.";
    encoding query;
    in X {
        type uint32;
        desc "X is the first number.";
    };
    in Y {
        type uint32;
        desc "Y is the second number.";
    };
    out Sum {
        type uint32;
        desc "Sum is the sum of the two numbers.";
    };
};

op Divide {
    desc "Divides two numbers.";
    encoding json;
    in X {
        type uint32;
        desc "X is the dividend.";
    };
    in Y {
        type uint32;
        desc "Y is the divisor.";
    };
    out Quotient {
        type uint32;
        desc "Quotient is the quotient of the division.";
    };
    out Remainder {
        type uint32;
        desc "Remainder is the remainder of the division.";
    };
    err ErrDivideByZero;
};

err ErrDivideByZero {
    desc "ErrDivideByZero is an error resulting from a division with a zero divisor.";
    text "division by zero";
    field Dividend {
        type uint32;
        desc "Dividend is the dividend of the erroneous division.";
    };
    code 400;
};
