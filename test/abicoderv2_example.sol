pragma solidity ^0.6.3;
pragma experimental ABIEncoderV2;

contract ExampleV2Coder {

    struct Type1 {
        string str1;
        uint232 val1;
        Type2 nested;
        Type2[] nestarray;
    }

    struct Type2 {
      string str1;
      string str2;
      address addr1;
      bytes bytearray;
    }

    function inOutType1(Type1 memory arg1) public pure returns(Type1 memory out1) {
        return arg1;
    }
}
