pragma solidity ^0.5.2;

/**
 * @title LotsOfTypes
 * @dev Challenges the swagger generator to use lots of types
 */
contract LotsOfTypes {

    uint256 val1;
    uint256 val2;
    uint256 val3;
    uint256 val4;
    uint256 val5;

    /**
      * @dev Echo back some types
      * @param param1 Parameter 1
      * @param param2 Parameter 2
      * @param param3 Parameter 3
      * @param param4 Parameter 4
      * @param param5 Parameter 5
      * @param param6 Parameter 6
      * @param param7 Parameter 7
      * @return all of the individual input parameters
      */
    function echoTypes1(
        uint8 param1,
        bytes memory param2,
        uint256[] memory param3,
        byte[] memory param4,
        bytes32 param5,
        bool[] memory param6,
        address[] memory param7
    )
    public
    pure
    returns (
        uint8   retval1,
        bytes memory retval2,
        uint256[] memory retval3,
        byte[] memory retval4,
        bytes32 retval5,
        bool[] memory retval6,
        address[] memory retval7
    )
    {
      return (param1,param2,param3,param4,param5,param6,param7);
    }

    /**
      * @dev Echo back some more types
      * @param param1 Parameter 1
      * @param param2 Parameter 2
      * @param param3 Parameter 3
      * @param param4 Parameter 4
      * @param param5 Parameter 5
      * @param param6 Parameter 6
      * @return all of the individual input parameters
      */
    function echoTypes2(
        string memory param1,
        int[] memory param2,
        bool param3,
        byte param4,
        address param5,
        bytes4 param6,
        uint256 param7
    )
    public
    pure
    returns (
        string memory retval1,
        int[] memory retval2,
        bool retval3,
        byte retval4,
        address retval5,
        bytes4 retval6,
        uint256 retval7
    )
    {
      return (param1,param2,param3,param4,param5,param6,param7);
    }

    function undocumentedWrites(
        uint256 param1,
        uint256 param2,
        uint256 param3,
        uint256 param4,
        uint256 param5,
        bool param6
    )
    public
    returns (
        uint256,
        uint256,
        uint256,
        uint256,
        uint256,
        bool
    )
    {
      val1 = param1;
      val2 = param2;
      val3 = param3;
      val4 = param4;
      val5 = param5;
      return (param1,param2,param3,param4,param5,param6);
    }

}
